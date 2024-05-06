package monitoringgateway

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/prometheus-community/prom-label-proxy/injectproxy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/model/labels"
	extpromhttp "github.com/thanos-io/thanos/pkg/extprom/http"
	"github.com/thanos-io/thanos/pkg/ui"
)

const (
	apiTenantPrefix = "/{tenant_id}/api/v1"
	apiGlobalPrefix = "/api/v1"
	apiUIPrefix     = "/-/ui"

	epQuery       = "/query"
	epQueryRange  = "/query_range"
	epSeries      = "/series"
	epLabels      = "/labels"
	epLabelValues = "/label/*path"
	epReceive     = "/receive"
	epRules       = "/rules"
	epAlerts      = "/alerts"
)

type Options struct {
	ListenAddress string
	TLSConfig     *tls.Config

	TenantHeader    string
	TenantLabelName string

	RemoteWriteHandler http.Handler
	QueryProxy         *httputil.ReverseProxy
	RulesQueryProxy    *httputil.ReverseProxy

	CertAuthenticator       *CertAuthenticator
	EnabledTenantsAdmission bool
}

type Handler struct {
	logger  log.Logger
	options *Options
	router  *mux.Router

	tenantsAdmissionMap sync.Map

	remoteWriteHander http.Handler
	queryProxy        *httputil.ReverseProxy
	rulesQueryProxy   *httputil.ReverseProxy
}

func NewHandler(logger log.Logger, o *Options) *Handler {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	h := &Handler{
		logger:            logger,
		options:           o,
		router:            mux.NewRouter(),
		remoteWriteHander: o.RemoteWriteHandler,
		queryProxy:        o.QueryProxy,
		rulesQueryProxy:   o.RulesQueryProxy,
	}

	// do provide /api/v1/alerts because thanos does not support alerts filtering as of v0.28.0
	// please filtering alerts by /api/v1/rules
	// h.router.Get(epAlerts, h.wrap(h.matcher(matchersParam)))
	h.addGlobalProxyHandler()
	h.addTenantQueryHandler()
	h.addTenantRemoteWriteHandler()
	return h
}

func (h *Handler) addTenantQueryHandler() {
	h.router.Path(apiTenantPrefix + epQuery).Methods(http.MethodGet).HandlerFunc(h.wrap(h.query))
	h.router.Path(apiTenantPrefix + epQuery).Methods(http.MethodPost).HandlerFunc(h.wrap(h.query))
	h.router.Path(apiTenantPrefix + epQueryRange).Methods(http.MethodGet).HandlerFunc(h.wrap(h.query))
	h.router.Path(apiTenantPrefix + epQueryRange).Methods(http.MethodPost).HandlerFunc(h.wrap(h.query))
	h.router.Path(apiTenantPrefix + epSeries).Methods(http.MethodGet).HandlerFunc(h.wrap(h.matcher(matchersParam)))
	h.router.Path(apiTenantPrefix + epLabels).Methods(http.MethodGet).HandlerFunc(h.wrap(h.matcher(matchersParam)))
	h.router.Path(apiTenantPrefix + epLabelValues).Methods(http.MethodGet).HandlerFunc(h.wrap(h.matcher(matchersParam)))
	h.router.Path(apiTenantPrefix + epRules).Methods(http.MethodGet).HandlerFunc(h.wrap(h.matcher(matchersParam)))
}
func (h *Handler) addTenantRemoteWriteHandler() {
	h.router.Path(apiTenantPrefix + epReceive).Methods(http.MethodPost).HandlerFunc(h.wrap(h.remoteWrite))
}

func (h *Handler) addGlobalProxyHandler() {
	if h.remoteWriteHander != nil {
		h.router.Path(apiGlobalPrefix + epReceive).HandlerFunc(h.remoteWriteHander.ServeHTTP)
	}
	if h.queryProxy != nil {
		h.router.PathPrefix(apiGlobalPrefix).HandlerFunc(h.queryProxy.ServeHTTP)
	}
}

func (h *Handler) AppendQueryUIHandler(logger log.Logger, reg *prometheus.Registry) {

	ins := extpromhttp.NewInstrumentationMiddleware(reg, nil)
	r := route.New()
	ui.NewQueryUI(logger, nil, "/-/ui", "", "", "", "", false).Register(r, ins)

	// matching /-/ui/* routes
	h.router.PathPrefix(apiUIPrefix).HandlerFunc(h.queryUIHander(apiUIPrefix, h.queryProxy.ServeHTTP, r.ServeHTTP))
}

func (h *Handler) queryUIHander(prefix string, queryHanler, uiHandler http.HandlerFunc) http.HandlerFunc {

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.URL.Path, _ = strings.CutPrefix(req.URL.Path, prefix)
		if strings.Contains(req.URL.Path, "/api/v1") {
			queryHanler.ServeHTTP(w, req)
		} else {
			uiHandler.ServeHTTP(w, req)
		}
	})
}

func (h *Handler) SetAdmissionControlHandler(c AdmissionControlConfig) error {
	if h.options.EnabledTenantsAdmission {
		v, ok := h.tenantsAdmissionMap.Load("/-/")
		if !ok || v == nil {
			level.Info(h.logger).Log("msg", "starting tenants admission control")
			h.tenantsAdmissionMap.Store("/-/", c.Tenants)
			for _, tenant := range c.Tenants {
				h.tenantsAdmissionMap.Store(tenant, true)
				level.Info(h.logger).Log("msg", fmt.Sprintf("tenant %s join admission queue", tenant))
			}
			return nil
		}
		tenants := v.([]string)
		addTenantset := difference(c.Tenants, tenants)
		for _, tenant := range addTenantset {
			h.tenantsAdmissionMap.Store(tenant, true)
			level.Info(h.logger).Log("msg", fmt.Sprintf("tenant %s join admission queue", tenant))

		}
		rmTenantset := difference(tenants, c.Tenants)
		for _, tenant := range rmTenantset {
			h.tenantsAdmissionMap.Delete(tenant)
			level.Info(h.logger).Log("msg", fmt.Sprintf("tenant %s is removed from the access queue", tenant))
		}
		h.tenantsAdmissionMap.Store("/-/", c.Tenants)
	}

	return nil
}

func (h *Handler) Router() *mux.Router {
	return h.router
}

func (h *Handler) wrap(f http.HandlerFunc) http.HandlerFunc {
	if h.options.CertAuthenticator != nil {
		f = withAuthorization(f, h.options.CertAuthenticator)
	}
	return withRequestInfo(f)
}

func (h *Handler) query(w http.ResponseWriter, req *http.Request) {
	if h.queryProxy == nil {
		http.Error(w, "The query target is not configured for the server", http.StatusNotAcceptable)
		return
	}

	var (
		query    = req.URL.Query()
		postForm url.Values
	)

	if req.Method == http.MethodPost {
		if err := req.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		postForm = req.PostForm
	}

	if query.Get(queryParam) == "" && postForm.Get(queryParam) == "" {
		return
	}

	ctx := req.Context()
	requestInfo, found := requestInfoFrom(ctx)

	if !found || requestInfo.TenantId == "" {
		http.NotFound(w, req)
		return
	}
	if _, ok := h.tenantsAdmissionMap.Load(requestInfo.TenantId); h.options.EnabledTenantsAdmission && !ok {
		err := fmt.Errorf("tenant %s is not allowed to access", requestInfo.TenantId)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	// Set errorOnReplace to false to directly replace the existing tenant with the new TenantId without reporting an error.
	enforcer := injectproxy.NewEnforcer(false, &labels.Matcher{
		Type:  labels.MatchEqual,
		Name:  h.options.TenantLabelName,
		Value: requestInfo.TenantId,
	})

	q, found, err := enforceQueryValues(enforcer, query)
	if err != nil {
		switch err.(type) {
		case injectproxy.IllegalLabelMatcherError:
			http.Error(w, err.Error(), http.StatusBadRequest)
		case queryParseError:
			http.Error(w, err.Error(), http.StatusBadRequest)
		case enforceLabelError:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if found {
		req.URL.RawQuery = q
	}

	if postForm != nil {
		q, found, err := enforceQueryValues(enforcer, postForm)
		if err != nil {
			switch err.(type) {
			case injectproxy.IllegalLabelMatcherError:
				http.Error(w, err.Error(), http.StatusBadRequest)
			case queryParseError:
				http.Error(w, err.Error(), http.StatusBadRequest)
			case enforceLabelError:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		if found {
			_ = req.Body.Close()
			req.Body = ioutil.NopCloser(strings.NewReader(q))
			req.ContentLength = int64(len(q))
		}
	}

	h.queryProxy.ServeHTTP(w, req)
}

func (h *Handler) matcher(matchersParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if h.queryProxy == nil {
			http.Error(w, "The query target is not configured for the server", http.StatusNotAcceptable)
			return
		}

		ctx := req.Context()
		requestInfo, found := requestInfoFrom(ctx)

		if !found || requestInfo.TenantId == "" {
			http.NotFound(w, req)
			return
		}
		if _, ok := h.tenantsAdmissionMap.Load(requestInfo.TenantId); h.options.EnabledTenantsAdmission && !ok {
			err := fmt.Errorf("tenant %s is not allowed to access", requestInfo.TenantId)
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		matcher := &labels.Matcher{
			Type:  labels.MatchEqual,
			Name:  h.options.TenantLabelName,
			Value: requestInfo.TenantId,
		}
		q := req.URL.Query()

		if err := injectMatcher(q, matcher, matchersParam); err != nil {
			return
		}
		req.URL.RawQuery = q.Encode()
		if req.Method == http.MethodPost {
			if err := req.ParseForm(); err != nil {
				return
			}
			q = req.PostForm
			if err := injectMatcher(q, matcher, matchersParam); err != nil {
				return
			}
			_ = req.Body.Close()
			req.Body = ioutil.NopCloser(strings.NewReader(q.Encode()))
			req.ContentLength = int64(len(q))
		}

		if (strings.HasSuffix(req.URL.Path, "/rules") || strings.HasSuffix(req.URL.Path, "/alerts")) &&
			h.rulesQueryProxy != nil {
			h.rulesQueryProxy.ServeHTTP(w, req)
			return
		}
		h.queryProxy.ServeHTTP(w, req)
	}
}

func (h *Handler) remoteWrite(w http.ResponseWriter, req *http.Request) {
	if h.remoteWriteHander == nil {
		http.Error(w, "There is no remote write targets configured for the server", http.StatusNotAcceptable)
		return
	}
	ctx := req.Context()
	requestInfo, found := requestInfoFrom(ctx)

	if !found || requestInfo.TenantId == "" {
		http.NotFound(w, req)
		return
	}
	if _, ok := h.tenantsAdmissionMap.Load(requestInfo.TenantId); h.options.EnabledTenantsAdmission && !ok {
		err := fmt.Errorf("tenant %s is not allowed to access", requestInfo.TenantId)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	req.Header.Set(h.options.TenantHeader, requestInfo.TenantId)

	h.remoteWriteHander.ServeHTTP(w, req)
}

func NewSingleHostReverseProxy(target *url.URL, transport http.RoundTripper) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)

	proxy.Transport = transport

	oldDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.Host = target.Host
		/*
			// remove the prefix /:tenant_id from path
			if index := indexByteNth(req.URL.Path, '/', 2); index > 0 {
				req.URL.Path = req.URL.Path[index:]
			}
		*/
		oldDirector(req)
	}

	return proxy
}

type remoteWriteHandler struct {
	writeClients []*remoteWriteClient
	tenantHeader string
}

func (h remoteWriteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if len(h.writeClients) == 0 {
		return
	}

	ctx := req.Context()

	// Forward the request to multiple targets in parallel.
	// If either forwarding fails, the errors are responded. This may result in repeated sending same data to one target.
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var results = make([]result, len(h.writeClients))
	var i = 0
	var wg sync.WaitGroup

	var tenantHeader = make(http.Header)
	if tenantId := req.Header.Get(h.tenantHeader); tenantId != "" {
		tenantHeader.Set(h.tenantHeader, tenantId)
	}
	for _, writeClient := range h.writeClients {
		wg.Add(1)
		ep := writeClient.Endpoint()

		go func(idx int, writeClient *remoteWriteClient) {
			defer wg.Done()
			result := writeClient.Send(ctx, body, tenantHeader)
			if result.err != nil {
				result.err = errors.Wrapf(result.err, "forwarding request to endpoint %v", ep)
			}
			results[idx] = result
		}(i, writeClient)
		i++
	}
	wg.Wait()

	var code int
	for _, result := range results {
		if result.code > code {
			code = result.code
			err = result.err
		}
	}
	if code <= 0 {
		code = http.StatusNoContent
	}
	if err != nil {
		http.Error(w, err.Error(), code)
	} else {
		w.WriteHeader(code)
	}
}

func NewRemoteWriteHandler(rwsCfg []RemoteWriteConfig, tenantHeader string) (http.Handler, error) {

	if len(rwsCfg) > 0 {
		var handler = remoteWriteHandler{tenantHeader: tenantHeader}
		for _, rwCfg := range rwsCfg {
			writeClient, err := newRemoteWriteClient(&rwCfg)
			if err != nil {
				return nil, err
			}
			if writeClient != nil {
				handler.writeClients = append(handler.writeClients, writeClient)
			}
		}
		return &handler, nil
	}

	return nil, nil
}

// indexByteNth returns the index of the nth instance of c in s, or -1 if the nth c is not present in s.
func indexByteNth(s string, c byte, nth int) int {
	num := 0
	for i, c := range s {
		if c == '/' {
			num++
			if num == nth {
				return i
			}
		}
	}
	return -1
}

func difference(a, b []string) []string {
	set := make([]string, 0)
	hash := make(map[string]struct{})

	for _, v := range b {
		hash[v] = struct{}{}
	}

	for _, v := range a {
		if _, ok := hash[v]; !ok {
			set = append(set, v)
		}
	}

	return set
}
