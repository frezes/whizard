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

package controllers

import (
	"context"
	"fmt"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/kubesphere/paodin-monitoring/pkg/api/v1alpha1"
	"github.com/kubesphere/paodin-monitoring/pkg/config"
	"github.com/kubesphere/paodin-monitoring/pkg/resources"
	"github.com/kubesphere/paodin-monitoring/pkg/resources/compact"
	"github.com/kubesphere/paodin-monitoring/pkg/resources/query"
	"github.com/kubesphere/paodin-monitoring/pkg/resources/receive"
	"github.com/kubesphere/paodin-monitoring/pkg/resources/storegateway"
)

// ThanosReconciler reconciles a Thanos object
type ThanosReconciler struct {
	DefaulterValidator ThanosDefaulterValidator
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=monitoring.paodin.io,resources=thanos,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.paodin.io,resources=thanos/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.paodin.io,resources=thanos/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Thanos object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *ThanosReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithValues("thanos", req.NamespacedName)

	l.Info("sync thanos")
	_ = sync.Once{}
	instance := &monitoringv1alpha1.Thanos{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	instance.Spec, err = r.DefaulterValidator(instance.Spec)
	if err != nil {
		return ctrl.Result{}, err
	}

	thanosBaseReconciler := resources.ThanosBaseReconciler{
		Client:  r.Client,
		Log:     l,
		Scheme:  r.Scheme,
		Context: ctx,
		Thanos:  instance,
	}

	var reconciles []func() error
	reconciles = append(reconciles, compact.New(thanosBaseReconciler).Reconcile)
	reconciles = append(reconciles, storegateway.New(thanosBaseReconciler).Reconcile)
	reconciles = append(reconciles, receive.New(thanosBaseReconciler).Reconcile)
	reconciles = append(reconciles, query.New(thanosBaseReconciler).Reconcile)
	for _, reconcile := range reconciles {
		if err := reconcile(); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ThanosReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.Thanos{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

type ThanosDefaulterValidator func(spec monitoringv1alpha1.ThanosSpec) (monitoringv1alpha1.ThanosSpec, error)

func CreateThanosDefaulterValidator(cfg config.Config) ThanosDefaulterValidator {
	var replicas int32 = 1
	var applyDefaultFields = func(defaultFields,
		fields monitoringv1alpha1.CommonThanosFields) monitoringv1alpha1.CommonThanosFields {
		if fields.Image == "" {
			fields.Image = defaultFields.Image
		}
		if fields.LogLevel == "" {
			fields.LogLevel = defaultFields.LogLevel
		}
		if fields.LogFormat == "" {
			fields.LogFormat = defaultFields.LogFormat
		}
		return fields
	}

	return func(spec monitoringv1alpha1.ThanosSpec) (monitoringv1alpha1.ThanosSpec, error) {
		if spec.DefaultFields.Image == "" {
			spec.DefaultFields.Image = cfg.ThanosDefaultImage
		}

		if spec.Query != nil {
			spec.Query.CommonThanosFields = applyDefaultFields(spec.DefaultFields, spec.Query.CommonThanosFields)
			if spec.Query.Replicas == nil || *spec.Query.Replicas < 0 {
				spec.Query.Replicas = &replicas
			}
			if spec.Query.Envoy.Image == "" {
				spec.Query.Envoy.Image = cfg.EnvoyDefaultImage
			}

		}
		if spec.Receive != nil {
			spec.Receive.Router.CommonThanosFields = applyDefaultFields(spec.DefaultFields, spec.Receive.Router.CommonThanosFields)
			if spec.Receive.Router.Replicas == nil || *spec.Receive.Router.Replicas < 0 {
				spec.Receive.Router.Replicas = &replicas
			}
			var ingestors []monitoringv1alpha1.ReceiveIngestor
			for _, i := range spec.Receive.Ingestors {
				ingestor := i
				if ingestor.Name == "" {
					return spec, fmt.Errorf("ingestor->name can not empty")
				}
				ingestor.CommonThanosFields = applyDefaultFields(spec.DefaultFields, ingestor.CommonThanosFields)
				if ingestor.Replicas == nil || *ingestor.Replicas < 0 {
					ingestor.Replicas = &replicas
				}
				ingestors = append(ingestors, ingestor)
			}
			spec.Receive.Ingestors = ingestors
		}
		if spec.StoreGateway != nil {
			spec.StoreGateway.CommonThanosFields = applyDefaultFields(spec.DefaultFields, spec.StoreGateway.CommonThanosFields)
			if spec.StoreGateway.Replicas == nil || *spec.StoreGateway.Replicas < 0 {
				spec.StoreGateway.Replicas = &replicas
			}
		}
		if spec.Compact != nil {
			spec.Compact.CommonThanosFields = applyDefaultFields(spec.DefaultFields, spec.Compact.CommonThanosFields)
			if spec.Compact.Replicas == nil || *spec.Compact.Replicas < 0 {
				spec.Compact.Replicas = &replicas
			}
		}
		return spec, nil
	}
}