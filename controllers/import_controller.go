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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	imagesetConfig "github.com/openshift/api/operator/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mirroropenshiftiov1alpha1 "github.com/redhat-et/magic-mirror/api/v1alpha1"
)

const (
	OrganizationName = "OCTO-ET"
	OrganizationUnit = "Platform"
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
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.openshift.io,resources=imagecontentsourcepolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authorization.openshift.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authorization.openshift.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

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

	foundSA := &corev1.ServiceAccount{}
	if err := r.Get(ctx, types.NamespacedName{Name: "kubectl-sa-" + instance.Name, Namespace: instance.Namespace}, foundSA); err != nil {
		if errors.IsNotFound(err) {
			// Define a new route
			sa := r.createCMSA(instance)
			klog.Info("Creating a new Service account ", sa.Namespace, " ", sa.Name)
			err = r.Create(ctx, sa)
			if err != nil {
				klog.Error(err, "Failed to create new Service account ", sa.Namespace, " ", sa.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: "kubectl-sa-" + instance.Name, Namespace: instance.Namespace}, foundSA); err != nil {
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
		klog.Error(err, "Failed to get Service account")
	}

	foundClusterRole := &rbacv1.ClusterRole{}
	if err := r.Get(ctx, types.NamespacedName{Name: "kubectl-sa-" + instance.Name}, foundClusterRole); err != nil {
		if errors.IsNotFound(err) {
			// Define a new cluster role
			clusterRole := r.createClusterRole(instance)
			klog.Info("Creating a new Cluster Role ", clusterRole.Name)
			err = r.Create(ctx, clusterRole)
			if err != nil {
				klog.Error(err, "Failed to create new Cluster Role ", clusterRole.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: "kubectl-sa-" + instance.Name}, foundClusterRole); err != nil {
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
			// Cluster Role created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		klog.Error(err, "Failed to get Cluster Role")
	}

	foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	if err := r.Get(ctx, types.NamespacedName{Name: "kubectl-sa-" + instance.Name}, foundClusterRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			// Define a new cluster role binding
			clusterRoleBinding := r.createClusterRoleBinding(instance)
			klog.Info("Creating a new Cluster Role Binding ", clusterRoleBinding.Name)
			err = r.Create(ctx, clusterRoleBinding)
			if err != nil {
				klog.Error(err, "Failed to create new Cluster Role Binding ", clusterRoleBinding.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: "kubectl-sa-" + instance.Name}, foundClusterRoleBinding); err != nil {
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
			// Cluster Role Binding created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		klog.Error(err, "Failed to get Cluster Role Binding")
	}

	foundRoute := &routev1.Route{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, foundRoute); err != nil {
		if errors.IsNotFound(err) {
			// Define a new route
			route := r.createRoute(instance)
			klog.Info("Creating a new Route ", route.Namespace, " ", route.Name)
			err = r.Create(ctx, route)
			if err != nil {
				klog.Error(err, "Failed to create new Route ", route.Namespace, " ", route.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, foundRoute); err != nil {
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
		klog.Error(err, "Failed to get Route")
	}

	foundIsc := &imagesetConfig.ImageContentSourcePolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: "sync-" + instance.Name, Namespace: instance.Namespace}, foundIsc); err != nil {
		if errors.IsNotFound(err) {
			// Get the route
			route := defineRoute(foundRoute)
			// Define a new isc
			isc := r.createImageSetConfig(instance, route)
			klog.Info("Creating a new ImageSetConfig ", isc.Namespace, " ", isc.Name)
			err = r.Create(ctx, isc)
			if err != nil {
				klog.Error(err, "Failed to create new ImageSetConfig ", isc.Namespace, " ", isc.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: "sync-" + instance.Name, Namespace: instance.Namespace}, foundIsc); err != nil {
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
			// ISC created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		klog.Error(err, "Failed to get ImageSetConfig")
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

	// Check if the deployment already exists, if not create a new one
	foundDeploy := &appsv1.Deployment{}
	if instance.Status.Synced {
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

	if isJobComplete(foundSync) {
		instance.Status.Synced = isJobComplete(foundSync)
		if err := r.Status().Update(ctx, instance); err != nil {
			klog.Error(err, "Failed to update Import status")
			return ctrl.Result{}, err
		}
		klog.Info("job cleanup")
		r.Delete(ctx, foundSync, client.PropagationPolicy(metav1.DeletePropagationForeground))
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
						Env: []corev1.EnvVar{
							{
								Name:  "REGISTRY_HTTP_ADDR",
								Value: "0.0.0.0:8443",
							},
							{
								Name:  "REGISTRY_HTTP_TLS_CERTIFICATE",
								Value: "/certs/ssl.crt",
							},
							{
								Name:  "REGISTRY_HTTP_TLS_KEY",
								Value: "/certs/ssl.key",
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "registry-data",
							MountPath: "/var/lib/registry",
						}, {
							Name:      "registry-certs",
							MountPath: "/certs",
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
						Name: "registry-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: m.Spec.CertificateSecret,
							},
						}}},
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
					ServiceAccountName: "kubectl-sa-" + m.Name,
					Containers: []corev1.Container{{
						Image:   "quay.io/disco-mirror/mirror-sync:latest",
						Name:    "sync-" + m.Name,
						Command: []string{"/usr/local/bin/import.sh"},
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

func (r *ImportReconciler) createCMSA(m *mirroropenshiftiov1alpha1.Import) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubectl-sa-" + m.Name,
			Namespace: m.Namespace,
		},
	}
	ctrl.SetControllerReference(m, sa, r.Scheme)
	return sa
}

func (r *ImportReconciler) createClusterRole(m *mirroropenshiftiov1alpha1.Import) *rbacv1.ClusterRole {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubectl-sa-" + m.Name,
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch"},
		}},
	}
	ctrl.SetControllerReference(m, clusterRole, r.Scheme)
	return clusterRole
}

func (r *ImportReconciler) createClusterRoleBinding(m *mirroropenshiftiov1alpha1.Import) *rbacv1.ClusterRoleBinding {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubectl-sa-" + m.Name,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "kubectl-sa-" + m.Name,
			Namespace: m.Namespace,
		}},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "kubectl-sa-" + m.Name,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	ctrl.SetControllerReference(m, clusterRoleBinding, r.Scheme)
	return clusterRoleBinding
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

func (r *ImportReconciler) createRoute(m *mirroropenshiftiov1alpha1.Import) *routev1.Route {
	// Define a new Route object
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: "service-" + m.Name,
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationPassthrough,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}

	// Route reconcile finished
	ctrl.SetControllerReference(m, route, r.Scheme)
	return route
}

func (r *ImportReconciler) serviceCreate(m *mirroropenshiftiov1alpha1.Import) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service-" + m.Name,
			Namespace: m.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Port: 8443,
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

func (r *ImportReconciler) createImageSetConfig(m *mirroropenshiftiov1alpha1.Import, route string) *imagesetConfig.ImageContentSourcePolicy {
	isc := &imagesetConfig.ImageContentSourcePolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ImageContentSourcePolicy",
			APIVersion: "operator.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "sync-" + m.Name,
		},
		Spec: imagesetConfig.ImageContentSourcePolicySpec{
			RepositoryDigestMirrors: []imagesetConfig.RepositoryDigestMirrors{
				{
					Source: "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
					Mirrors: []string{
						route + "/ocp4/openshift4/openshift/release",
					},
				},
				{
					Source: "quay.io/openshift-release-dev/ocp-release",
					Mirrors: []string{
						route + "/ocp4/openshift4/openshift/release-images",
					},
				}},
		},
	}
	ctrl.SetControllerReference(m, isc, r.Scheme)
	return isc
}

// Identify route to be used for status
func defineRoute(route *routev1.Route) string {
	return route.Spec.Host
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
