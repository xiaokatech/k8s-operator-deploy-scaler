/*
Copyright 2025.

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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	apiv1alpha1 "github.com/xiaokatech/k8s-operator-deploy-scaler/api/v1alpha1"
)

var logger = logf.Log.WithName("scaler_controller")

var originalDeploymentInfo = make(map[string]apiv1alpha1.DeploymentInfo)
var annotations = make(map[string]string)

// ScalerReconciler reconciles a Scaler object
type ScalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=api.scaler.com,resources=scalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=api.scaler.com,resources=scalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=api.scaler.com,resources=scalers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Scaler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ScalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx,
		"Request.Namespace", req.Namespace,
		"Request.Name", req.Name,
	)
	log.Info("Reconcile called")

	// Create a Scaler instance
	scaler := &apiv1alpha1.Scaler{}
	err := r.Get(ctx, req.NamespacedName, scaler)
	if err != nil {
		// use client.IgnoreNotFound to ignore not func instance error if we don't have the Scaler instance, to avoid interruption of program
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	startTime := scaler.Spec.Start
	endTime := scaler.Spec.End
	replicas := scaler.Spec.Replicas

	currentHour := time.Now().Hour()
	log.Info(fmt.Sprintf("currentTime: %d", currentHour))

	// Check if in the period of startTime and endTime
	if currentHour >= startTime && currentHour < endTime {
		log.Info("starting to call scaleDeployment func")
		err := scaleDeployment(scaler, r, ctx, replicas)
		if err != nil {
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{RequeueAfter: time.Duration(10 * time.Second)}, nil
}

func scaleDeployment(scaler *apiv1alpha1.Scaler, r *ScalerReconciler, ctx context.Context, replicas int32) error {
	// Loop deployments in scaler instance
	for _, deploy := range scaler.Spec.Deployments {
		// Create a new Deployment instance from scaler instance
		deployment := &v1.Deployment{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      deploy.Name,
			Namespace: deployment.Namespace,
		}, deployment)
		if err != nil {
			return err
		}

		// Check if the deployment copies num is equal to deployment copies num defined in scaler in current k8s cluster
		if deployment.Spec.Replicas != &replicas {
			deployment.Spec.Replicas = &replicas
			err := r.Update(ctx, deployment)
			if err != nil {
				scaler.Status.Status = apiv1alpha1.FAILED
				r.Status().Update(ctx, scaler)
				return err
			}

			scaler.Status.Status = apiv1alpha1.SCALED
			r.Status().Update(ctx, scaler)
		}
	}

	return nil
}

func addAnnotations(scaler *apiv1alpha1.Scaler, r *ScalerReconciler, ctx context.Context) error {

	// Note deployments original replicas and namespace
	for _, deploy := range scaler.Spec.Deployments {
		deployment := &v1.Deployment{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      deploy.Name,
			Namespace: deploy.Namespace,
		}, deployment); err != nil {
			return err
		}

		// Start note
		if *deployment.Spec.Replicas != scaler.Spec.Replicas {
			logger.Info("add original state to originalDeploymentInfo map")
			originalDeploymentInfo[deployment.Name] = apiv1alpha1.DeploymentInfo{
				Replicas:  *deployment.Spec.Replicas,
				Namespace: deployment.Namespace,
			}
		}
	}

	// Add originalDeploymentInfo into annotations
	for deploymentName, info := range originalDeploymentInfo {
		// Convert into into json format
		infoJson, err := json.Marshal(info)
		if err != nil {
			return err
		}

		// Save infoJson into annotations map
		annotations[deploymentName] = string(infoJson)
	}

	// Update CRD annotations
	scaler.ObjectMeta.Annotations = annotations
	err := r.Update(ctx, scaler)
	if err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1alpha1.Scaler{}).
		Named("scaler").
		Complete(r)
}
