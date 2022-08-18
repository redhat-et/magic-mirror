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
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

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
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

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
			klog.Info("Import resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		klog.Error(err, "Failed to get Import")
		return ctrl.Result{}, err
	}

	foundSync := &batchv1.Job{}
	if !instance.Status.Synced {
		if err := r.Get(ctx, types.NamespacedName{Name: "sync-" + instance.Name, Namespace: instance.Namespace}, foundSync); err != nil {
			if errors.IsNotFound(err) {
				// Define a new deployment
				sync := r.syncJob(instance)
				klog.Info("Creating a new job ", sync.Namespace, " ", sync.Name)
				err = r.Create(ctx, sync)
				if err != nil {
					klog.Error(err, "Failed to create new job ", sync.Namespace, " ", sync.Name)
					return ctrl.Result{}, err
				}
				if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
					if err := r.Get(ctx, types.NamespacedName{Name: "sync-" + instance.Name, Namespace: instance.Namespace}, foundSync); err != nil {
						if errors.IsNotFound(err) {
							return false, nil
						} else {
							return false, err
						}
					}
					return true, nil
				}); err != nil {
					return ctrl.Result{}, err
				}
				// Service created successfully - return and requeue
				return ctrl.Result{Requeue: true}, nil
			}
			klog.Error(err, "Failed to get job")
		}
	}

	foundMirror := &batchv1.Job{}
	if instance.Status.Synced && !instance.Status.Mirrored {
		if err := r.Get(ctx, types.NamespacedName{Name: "mirror-" + instance.Name, Namespace: instance.Namespace}, foundMirror); err != nil {
			if errors.IsNotFound(err) {
				// Define a new deployment
				mirror := r.mirrorJob(instance)
				klog.Info("Creating a new job ", mirror.Namespace, " ", mirror.Name)
				err = r.Create(ctx, mirror)
				if err != nil {
					klog.Error(err, "Failed to create new job ", mirror.Namespace, " ", mirror.Name)
					return ctrl.Result{}, err
				}
				if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
					if err := r.Get(ctx, types.NamespacedName{Name: "mirror-" + instance.Name, Namespace: instance.Namespace}, foundMirror); err != nil {
						if errors.IsNotFound(err) {
							return false, nil
						} else {
							return false, err
						}
					}
					return true, nil
				}); err != nil {
					return ctrl.Result{}, err
				}
				// Service created successfully - return and requeue
				return ctrl.Result{Requeue: true}, nil
			}
			klog.Error(err, "Failed to get job")
		}
	}

	foundPVC := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: "pvc-" + instance.Name, Namespace: instance.Namespace}, foundPVC); err != nil {
		if errors.IsNotFound(err) {
			// Define a new deployment
			pvc := r.pvcCreate(instance)
			klog.Info("Creating a new PVC ", pvc.Namespace, " ", pvc.Name)
			err = r.Create(ctx, pvc)
			if err != nil {
				klog.Error(err, "Failed to create new PVC ", pvc.Namespace, " ", pvc.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: "pvc-" + instance.Name, Namespace: instance.Namespace}, foundPVC); err != nil {
					if errors.IsNotFound(err) {
						return false, nil
					} else {
						return false, err
					}
				}
				return true, nil
			}); err != nil {
				return ctrl.Result{}, err
			}
			// Service created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		klog.Error(err, "Failed to get PVC")
	}

	foundService := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: "service-" + instance.Name, Namespace: instance.Namespace}, foundService); err != nil {
		if errors.IsNotFound(err) {
			// Define a new deployment
			svc := r.serviceCreate(instance)
			klog.Info("Creating a new Service ", svc.Namespace, " ", svc.Name)
			err = r.Create(ctx, svc)
			if err != nil {
				klog.Error(err, "Failed to create new Service ", svc.Namespace, " ", svc.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: "service-" + instance.Name, Namespace: instance.Namespace}, foundService); err != nil {
					if errors.IsNotFound(err) {
						return false, nil
					} else {
						return false, err
					}
				}
				return true, nil
			}); err != nil {
				return ctrl.Result{}, err
			}
			// Service created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		klog.Error(err, "Failed to get Service")
	}

	// Check if the deployment already exists, if not create a new one
	foundDeploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, foundDeploy); err != nil {
		if errors.IsNotFound(err) {
			// Define a new deployment
			dep := r.deployRegistry(instance)
			klog.Info("Creating a new Deployment ", dep.Namespace, " ", dep.Name)
			err = r.Create(ctx, dep)
			if err != nil {
				klog.Error(err, "Failed to create new Deployment ", dep.Namespace, "Deployment.Name", dep.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, foundDeploy); err != nil {
					if errors.IsNotFound(err) {
						return false, nil
					} else {
						return false, err
					}
				}
				return true, nil
			}); err != nil {
				return ctrl.Result{}, err
			}
			// Service created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		klog.Error(err, "Failed to get Deployment")
	}

	if isJobComplete(foundSync) {
		instance.Status.Synced = isJobComplete(foundSync)
		if err := r.Status().Update(ctx, instance); err != nil {
			klog.Error(err, "Failed to update Import status")
			return ctrl.Result{}, err
		}
		klog.Info("job cleanup")
		r.Delete(ctx, foundSync, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}

	if isJobComplete(foundMirror) {
		instance.Status.Mirrored = isJobComplete(foundMirror)
		if err := r.Status().Update(ctx, instance); err != nil {
			klog.Error(err, "Failed to update Import status")
			return ctrl.Result{}, err
		}
		klog.Info("job cleanup")
		r.Delete(ctx, foundMirror, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}

	return ctrl.Result{Requeue: true}, nil
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
						Image: "registry:2",
						Name:  "registry",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 5000,
							Name:          "registry",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "pvc-" + m.Name,
							},
						},
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
						Image:   "quay.io/disco-mirror/mirror-sync:latest",
						Name:    "sync" + m.Name,
						Command: []string{"/usr/local/bin/sync.sh"},
						Env: []corev1.EnvVar{{
							Name:  "SOURCETYPE",
							Value: m.Spec.SourceType,
						}, {
							Name:  "SOURCE",
							Value: m.Spec.Source,
						}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "data",
							MountPath: "/data",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "pvc-" + m.Name,
							},
						},
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
	// make the pvc size a string
	size := strconv.Itoa(m.Spec.PvcSize)
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
					corev1.ResourceName(corev1.ResourceStorage): resource.MustParse(size + "Gi"),
				},
			},
		},
	}
	ctrl.SetControllerReference(m, pvc, r.Scheme)
	return pvc
}

func (r *ImportReconciler) serviceCreate(m *mirroropenshiftiov1alpha1.Import) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service-" + m.Name,
			Namespace: m.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Port: 5000,
				Name: "registry",
			}},
			Selector: map[string]string{
				"mirror": "registry",
			},
		},
	}
	ctrl.SetControllerReference(m, service, r.Scheme)
	return service
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
						Image:   "quay.io/disco-mirror/oc-mirror:latest",
						Name:    "mirror" + m.Name,
						Command: []string{"/bin/sh", "-c", "/usr/bin/oc-mirror --from /data/oc-mirror/mirror_seq1_000000.tar docker://service-import-sample.default.svc.cluster.local:5000 --dest-skip-tls -v 1"},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "data",
							MountPath: "/data",
						},
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "pvc-" + m.Name,
							},
						},
					},
					}, RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
	ctrl.SetControllerReference(m, mirrorJob, r.Scheme)
	return mirrorJob
}

// Check to see if job is completed
func isJobComplete(job *batchv1.Job) bool {
	klog.Info("Checking if job is complete")
	return job.Status.Succeeded == 1
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mirroropenshiftiov1alpha1.Import{}).
		Complete(r)
}
