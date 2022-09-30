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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mirroropenshiftiov1alpha1 "github.com/redhat-et/magic-mirror/api/v1alpha1"
)

// ExportReconciler reconciles a Export object
type ExportReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=mirror.openshift.io,resources=exports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mirror.openshift.io,resources=exports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mirror.openshift.io,resources=exports/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.openshift.io,resources=imagecontentsourcepolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Export object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *ExportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	instance := &mirroropenshiftiov1alpha1.Export{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("Export resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		klog.Error(err, "Failed to get Export")
		return ctrl.Result{}, err
	}

	foundMirror := &batchv1.Job{}
	if instance.Status.RegistryOnline {
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

	foundSync := &batchv1.Job{}
	if instance.Status.RegistryOnline && instance.Status.Mirrored {
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

	foundService := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: "service-" + instance.Name, Namespace: instance.Namespace}, foundService); err != nil {
		if errors.IsNotFound(err) {
			// Define a new service
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

	// Check if the deployment already exists, if not create a new one
	foundDeploy := &appsv1.Deployment{}
	if !instance.Status.Mirrored {
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
	}

	if isJobComplete(foundMirror) {
		instance.Status.Mirrored = isJobComplete(foundMirror)
		if err := r.Status().Update(ctx, instance); err != nil {
			klog.Error(err, "Failed to update Export status")
			return ctrl.Result{}, err
		}
		klog.Info("job cleanup")
		r.Delete(ctx, foundMirror, client.PropagationPolicy(metav1.DeletePropagationForeground))
		r.Delete(ctx, foundDeploy, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}

	if isJobComplete(foundSync) {
		instance.Status.Synced = isJobComplete(foundSync)
		if err := r.Status().Update(ctx, instance); err != nil {
			klog.Error(err, "Failed to update Export status")
			return ctrl.Result{}, err
		}
		klog.Info("job cleanup")
		r.Delete(ctx, foundSync, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}

	if isDeploymentReady(foundDeploy) {
		instance.Status.RegistryOnline = isDeploymentReady(foundDeploy)
		if err := r.Status().Update(ctx, instance); err != nil {
			klog.Error(err, "Failed to update Export status")
			return ctrl.Result{}, err
		}
		klog.Info("Registry is online")
	}

	return ctrl.Result{Requeue: true}, nil
}

func (r *ExportReconciler) deployRegistry(m *mirroropenshiftiov1alpha1.Export) *appsv1.Deployment {
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
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "registry-data",
							MountPath: "/var/lib/registry",
						}},
						Ports: []corev1.ContainerPort{{
							ContainerPort: 5000,
							Name:          "registry",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "registry-data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "pvc-" + m.Name,
							},
						},
					}, {
						Name: "secret-" + m.Name,
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "secret-" + m.Name,
							},
						}}},
				},
			},
		},
	}
	ctrl.SetControllerReference(m, dep, r.Scheme)
	return dep
}

func (r *ExportReconciler) syncJob(m *mirroropenshiftiov1alpha1.Export) *batchv1.Job {
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
						Name:    "sync-" + m.Name,
						Command: []string{"/usr/local/bin/export.sh"},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "data",
							MountPath: "/data",
						}},
						Env: []corev1.EnvVar{{
							Name:  "PROVIDERTYPE",
							Value: m.Spec.ProviderType,
						}, {
							Name:  "STORAGEOBJECT",
							Value: m.Spec.StorageObject,
						}, {
							Name: "AWS_ACCESS_KEY",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: m.Spec.CredSecret,
									},
									Key: "AWS_ACCESS_KEY",
								},
							}}, {
							Name: "AWS_SECRET_ACCESS_KEY",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: m.Spec.CredSecret,
									},
									Key: "AWS_SECRET_ACCESS_KEY",
								},
							},
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

func (r *ExportReconciler) mirrorJob(m *mirroropenshiftiov1alpha1.Export) *batchv1.Job {
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
						Command: []string{"/bin/sh", "-c", "/usr/bin/oc-mirror --config /opt/imageset-configuration.yaml docker://service-" + m.Name + "." + m.Namespace + ".svc.cluster.local:5000 --dest-skip-tls -v 1"},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "image-set-config",
							MountPath: "/opt/",
						}, {
							Name:      "data",
							MountPath: "/data",
						}, {
							Name:      "docker-config",
							MountPath: "/root/.docker",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "image-set-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: m.Spec.ImageSetConfiguration,
								}},
						},
					}, {
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "pvc-" + m.Name,
							},
						}}, {
						Name: "docker-config",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: m.Spec.DockerConfigSecret,
							},
						}}},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
	ctrl.SetControllerReference(m, mirrorJob, r.Scheme)
	return mirrorJob
}

func (r *ExportReconciler) serviceCreate(m *mirroropenshiftiov1alpha1.Export) *corev1.Service {
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

func (r *ExportReconciler) pvcCreate(m *mirroropenshiftiov1alpha1.Export) *corev1.PersistentVolumeClaim {
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

func isDeploymentReady(deployment *appsv1.Deployment) bool {
	return deployment.Status.ReadyReplicas == 1
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mirroropenshiftiov1alpha1.Export{}).
		Complete(r)
}
