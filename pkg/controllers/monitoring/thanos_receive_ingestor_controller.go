/*
Copyright 2021 The KubeSphere authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package monitoring

import (
	"context"
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	monitoringv1alpha1 "github.com/kubesphere/paodin/pkg/api/monitoring/v1alpha1"
	"github.com/kubesphere/paodin/pkg/controllers/monitoring/options"
	"github.com/kubesphere/paodin/pkg/controllers/monitoring/resources"
	receive_ingester "github.com/kubesphere/paodin/pkg/controllers/monitoring/resources/receive_ingestor"
	"github.com/prometheus/common/model"
)

// ThanosReceiveIngesterReconciler reconciles a ThanosReceiveIngester object
type ThanosReceiveIngesterReconciler struct {
	DefaulterValidator ThanosReceiveIngesterDefaulterValidator
	client.Client
	Scheme  *runtime.Scheme
	Context context.Context
}

//+kubebuilder:rbac:groups=monitoring.paodin.io,resources=thanosreceiveingesters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.paodin.io,resources=thanosreceiveingesters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.paodin.io,resources=thanosreceiveingesters/finalizers,verbs=update
//+kubebuilder:rbac:groups=monitoring.paodin.io,resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Service object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *ThanosReceiveIngesterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithValues("thanosreceiveingester", req.NamespacedName)

	l.Info("sync")

	instance := &monitoringv1alpha1.Ingester{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// recycle ingester by using the RequeueAfter event
	if v, ok := instance.Annotations[resources.LabelNameReceiveIngesterState]; ok && v == "deleting" && len(instance.Spec.Tenants) == 0 {
		if deletingTime, ok := instance.Annotations[resources.LabelNameReceiveIngesterDeletingTime]; ok {
			i, err := strconv.ParseInt(deletingTime, 10, 64)
			if err == nil {
				d := time.Since(time.Unix(i, 0))
				if d < 0 {
					l.Info("recycle", "recycled time", (-d).String())
					return ctrl.Result{Requeue: true, RequeueAfter: (-d)}, nil
				} else {
					err := r.Delete(r.Context, instance)
					return ctrl.Result{}, err
				}
			}
		}
	}

	instance, err = r.DefaulterValidator(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	baseReconciler := resources.BaseReconciler{
		Client:  r.Client,
		Log:     l,
		Scheme:  r.Scheme,
		Context: ctx,
	}

	if err := receive_ingester.New(baseReconciler, instance).Reconcile(); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ThanosReceiveIngesterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.Ingester{}).
		Watches(&source.Kind{Type: &monitoringv1alpha1.Service{}},
			handler.EnqueueRequestsFromMapFunc(r.mapToIngesterFunc)).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

func (r *ThanosReceiveIngesterReconciler) mapToIngesterFunc(o client.Object) []reconcile.Request {

	var ingesterList monitoringv1alpha1.IngesterList
	if err := r.Client.List(r.Context, &ingesterList,
		client.MatchingLabels(monitoringv1alpha1.ManagedLabelByService(o))); err != nil {
		log.FromContext(r.Context).WithValues("thanosreceiveingesterlist", "").Error(err, "")
		return nil
	}

	var reqs []reconcile.Request
	for _, ingester := range ingesterList.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ingester.Namespace,
				Name:      ingester.Name,
			},
		})
	}

	return reqs
}

type ThanosReceiveIngesterDefaulterValidator func(ingester *monitoringv1alpha1.Ingester) (*monitoringv1alpha1.Ingester, error)

func CreateThanosReceiveIngesterDefaulterValidator(opt options.Options) ThanosReceiveIngesterDefaulterValidator {
	var replicas int32 = 1

	return func(ingester *monitoringv1alpha1.Ingester) (*monitoringv1alpha1.Ingester, error) {

		if ingester.Spec.LocalTsdbRetention != "" {
			_, err := model.ParseDuration(ingester.Spec.LocalTsdbRetention)
			if err != nil {
				return nil, fmt.Errorf("invalid localTsdbRetention: %v", err)
			}
		}

		if ingester.Spec.Image == "" {
			ingester.Spec.Image = opt.ThanosImage
		}
		if ingester.Spec.Replicas == nil || *ingester.Spec.Replicas < 0 {
			ingester.Spec.Replicas = &replicas
		}

		return ingester, nil
	}
}
