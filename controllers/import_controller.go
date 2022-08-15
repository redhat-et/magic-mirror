/*
Copyright 2022.

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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mirroropenshiftiov1alpha1 "github.com/redhat-et/magic-mirror/api/v1alpha1"
)

// ImportReconciler reconciles a Import object
type ImportReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=mirror.openshift.io,resources=imports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mirror.openshift.io,resources=imports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mirror.openshift.io,resources=imports/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Import object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *ImportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	instance := &mirroropenshiftiov1alpha1.Import{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			klog.Info("Import resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		klog.Error(err, "Failed to get Import")
		return ctrl.Result{}, err
	}

	foundSync := &batchv1.Job{}
	if !instance.Status.Synced {
		err = r.Get(ctx, types.NamespacedName{Name: "sync-" + instance.Name, Namespace: instance.Namespace}, foundSync)
		if err != nil && errors.IsNotFound(err) {
			// Define a new Job
			syncJob := r.syncJob(instance)
			klog.Info("Creating a new Job ", syncJob.Namespace, " ", syncJob.Name)
			err = r.Create(ctx, syncJob)
			if err != nil {
				klog.Error(err, "Failed to create new Job ", syncJob.Namespace, " ", syncJob.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			klog.Error(err, "Failed to get Job")
			return ctrl.Result{}, err
		}
	}

	foundMirror := &batchv1.Job{}
	if instance.Status.Synced {
		err = r.Get(ctx, types.NamespacedName{Name: "mirror-" + instance.Name, Namespace: instance.Namespace}, foundMirror)
		if err != nil && errors.IsNotFound(err) {
			// Define a new Job
			mirrorJob := r.mirrorJob(instance)
			klog.Info("Creating a new Job ", mirrorJob.Namespace, " ", mirrorJob.Name)
			err = r.Create(ctx, mirrorJob)
			if err != nil {
				klog.Error(err, "Failed to create new Job ", mirrorJob.Namespace, " ", mirrorJob.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			klog.Error(err, "Failed to get Job")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	foundPVC := &corev1.PersistentVolumeClaim{}
	err = r.Get(ctx, types.NamespacedName{Name: "pvc-" + instance.Name, Namespace: instance.Namespace}, foundPVC)
	if err != nil && errors.IsNotFound(err) {
		// Define a new Job
		pvc := r.pvcCreate(instance)
		klog.Info("Creating a new PVC ", pvc.Namespace, " ", pvc.Name)
		err = r.Create(ctx, pvc)
		if err != nil {
			klog.Error(err, "Failed to create new PVC ", pvc.Namespace, " ", pvc.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		klog.Error(err, "Failed to get Job")
		return ctrl.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	foundDeploy := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, foundDeploy)
	if err != nil && errors.IsNotFound(err) {
		// Define a new deployment
		dep := r.deployRegistry(instance)
		klog.Info("Creating a new Deployment ", dep.Namespace, " ", dep.Name)
		err = r.Create(ctx, dep)
		if err != nil {
			klog.Error(err, "Failed to create new Deployment ", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}
		// Deployment created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		klog.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// If the sync job is completed update the Synced bool to be true
	if isJobComplete(foundSync) {
		instance.Status.Synced = true
		klog.Info("job cleanup")
		r.Delete(ctx, foundSync, client.PropagationPolicy(metav1.DeletePropagationForeground))
		err = r.Status().Update(ctx, instance)
		if err != nil {
			klog.Error(err, "Failed to update Import status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	} else {
		instance.Status.Synced = false
		err = r.Status().Update(ctx, instance)
		if err != nil {
			klog.Error(err, "Failed to update Import status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
}

func (r *ImportReconciler) deployRegistry(m *mirroropenshiftiov1alpha1.Import) *appsv1.Deployment {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"mirror": "registry",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"mirror": "registry",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:   "memcached:1.4.36-alpine",
						Name:    "memcached",
						Command: []string{"memcached", "-m=64", "-o", "modern", "-v"},
						Ports: []corev1.ContainerPort{{
							ContainerPort: 11211,
							Name:          "memcached",
						}},
					}},
				},
			},
		},
	}
	ctrl.SetControllerReference(m, dep, r.Scheme)
	return dep
}

func (r *ImportReconciler) syncJob(m *mirroropenshiftiov1alpha1.Import) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sync-" + m.Name,
			Namespace: m.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"mirror": "sync",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:   "alpine",
						Name:    "sync" + m.Name,
						Command: []string{"/bin/sh", "-c", "sleep 10"},
					}},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
	ctrl.SetControllerReference(m, job, r.Scheme)
	return job
}

func (r *ImportReconciler) pvcCreate(m *mirroropenshiftiov1alpha1.Import) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-" + m.Name,
			Namespace: m.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): resource.MustParse("1Gi"),
				},
			},
		},
	}
	ctrl.SetControllerReference(m, pvc, r.Scheme)
	return pvc
}

func (r *ImportReconciler) mirrorJob(m *mirroropenshiftiov1alpha1.Import) *batchv1.Job {
	mirrorJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mirror-" + m.Name,
			Namespace: m.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"mirror": "sync",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:   "memcached:1.4.36-alpine",
						Name:    "memcached",
						Command: []string{"memcached", "-m=64", "-o", "modern", "-v"},
					}},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
	ctrl.SetControllerReference(m, mirrorJob, r.Scheme)
	return mirrorJob
}

// Check to see if job is completed
func isJobComplete(job *batchv1.Job) bool {
	return job.Status.Succeeded == 1
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mirroropenshiftiov1alpha1.Import{}).
		Complete(r)
}
