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

	foundRoute := &routev1.Route{}
	if err := r.Get(ctx, types.NamespacedName{Name: "sync-" + instance.Name, Namespace: instance.Namespace}, foundRoute); err != nil {
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
				if err := r.Get(ctx, types.NamespacedName{Name: "sync-" + instance.Name, Namespace: instance.Namespace}, foundRoute); err != nil {
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

	foundSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: "secret-" + instance.Name, Namespace: instance.Namespace}, foundSecret); err != nil {
		if errors.IsNotFound(err) {
			// Define a new Secret
			secret := r.secretCreate(instance)
			klog.Info("Creating a new Secret ", secret.Namespace, " ", secret.Name)
			err = r.Create(ctx, secret)
			if err != nil {
				klog.Error(err, "Failed to create new Secret ", secret.Namespace, " ", secret.Name)
				return ctrl.Result{}, err
			}
			if err := wait.Poll(time.Second*1, time.Second*15, func() (done bool, err error) {
				if err := r.Get(ctx, types.NamespacedName{Name: "secret-" + instance.Name, Namespace: instance.Namespace}, foundSecret); err != nil {
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
		klog.Error(err, "Failed to get Secret")
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
							Name:      "secret-" + m.Name,
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
						Name:    "sync-" + m.Name,
						Command: []string{"/usr/local/bin/sync.sh"},
						Env: []corev1.EnvVar{{
							Name:  "SOURCETYPE",
							Value: m.Spec.SourceType,
						}, {
							Name:  "SOURCE",
							Value: m.Spec.Source,
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
			Name:      "sync-" + m.Name,
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

func (r *ImportReconciler) secretCreate(m *mirroropenshiftiov1alpha1.Import) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-" + m.Name,
			Namespace: m.Namespace,
		},
		Data: map[string][]byte{
			"ssl.crt": []byte("-----BEGIN CERTIFICATE-----MIIEBDCCAuygAwIBAgIUOf4GkyYm/WhipO0535wU7SO5IN0wDQYJKoZIhvcNAQELBQAwgY8xCzAJBgNVBAYTAlVTMRcwFQYDVQQIDA5Ob3J0aCBDYXJvbGluYTEPMA0GA1UEBwwGRHVyaGFtMRwwGgYDVQQKDBNEZWZhdWx0IENvbXBhbnkgTHRkMTgwNgYDVQQDDC9zZXJ2aWNlLWltcG9ydC1zYW1wbGUuZGVmYXVsdC5zdmMuY2x1c3Rlci5sb2NhbDAeFw0yMjA5MjYxODM2NTlaFw0yMzA5MTcxODM2NTlaMIGUMQswCQYDVQQGEwJVUzEXMBUGA1UECAwOTm9ydGggQ2Fyb2xpbmExDzANBgNVBAcMBkR1cmhhbTEcMBoGA1UECgwTRGVmYXVsdCBDb21wYW55IEx0ZDE9MDsGA1UEAww0c2VydmljZS1pbXBvcnQtc2FtcGxlLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWw6ODQ0MzCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAK9PNn9NAfQvMuaKtPs5P5yWtxnLeapCYhvTR/+eU71rsTeCAJ/rFlyq/s6U0UNXiQYIbYGeiaWRx9dY1iQfqf+MdaU/sROEi0PSFFP6YEqZIa9PnqHTvx0pWH5z/AmLGvsqGly4APvxLU5i4RXeEkkucSYNfj/qUmJp+wmevEQ3stB8QFzX7DzZuAI1gIQifTAEsPVAaJj6a7kl/nlkauFlG+o2CS8C4/MfGQLKUghAQbjWvJZdXlLLqR5widS733Yf2taAjsPiT3ohEllvBYzyDgU3m5BapJbsUu0yzCs9wpHpAwgf7N3q/2kYGGIlPtzCE4qFyf7sazt2MtPQMQUCAwEAAaNRME8wCQYDVR0TBAIwADALBgNVHQ8EBAMCBeAwNQYDVR0RBC4wLIIkcmVnZ2llLm9jdG8tZW1lcmdpbmcucmVkaGF0YWljb2UuY29thwQDXxi9MA0GCSqGSIb3DQEBCwUAA4IBAQCHqYLqNavK4TCyn1F3Ai2ydYBf6z3YYBAj7fxp7claWJFy/qp5WV4/CPnUo9ollQhWH5IQEDmL3n6fV2VOyJZN/kI2mpF2Ysad2GTy6Nbc6FSQ8UKZPoNjzmzDFPfRRUkejmB/hdfH1k/u0AVG2///hESU4492PT9ozdKE0UEokD1R639smrTeizqgR9s9iNlC5fWkPTTXWSrA/hi8aWm/r/L0QV/f7jtdqgO2cUayaQkAgSz/aaBQDRi590AsiaI6L3XufDtUfWmWFXsgdkRJGiExbGFYQqpddxmaa8FfaByBfeocqwv1UvZ6XBqg4ZqqESxSkso+DeASHD7S0a7X-----END CERTIFICATE-----"),
			"ssl.key": []byte("-----BEGIN RSA PRIVATE KEY-----MIIEowIBAAKCAQEAr082f00B9C8y5oq0+zk/nJa3Gct5qkJiG9NH/55TvWuxN4IAn+sWXKr+zpTRQ1eJBghtgZ6JpZHH11jWJB+p/4x1pT+xE4SLQ9IUU/pgSpkhr0+eodO/HSlYfnP8CYsa+yoaXLgA+/EtTmLhFd4SSS5xJg1+P+pSYmn7CZ68RDey0HxAXNfsPNm4AjWAhCJ9MASw9UBomPpruSX+eWRq4WUb6jYJLwLj8x8ZAspSCEBBuNa8ll1eUsupHnCJ1Lvfdh/a1oCOw+JPeiESWW8FjPIOBTebkFqkluxS7TLMKz3CkekDCB/s3er/aRgYYiU+3MITioXJ/uxrO3Yy09AxBQIDAQABAoIBABsDtt8pC7sIJuzVxQvNh5rmsrJ743S0JBArn7WpPTg8RyPJmbUK8fg3tWo6DoE1FP1kARPvTUDBVS0/GEiaxISHrX1Ycj4St68syUsjkwEL1eABAe3oBlRFEcjysIz77Z10oHlXNXedc6DXpd3Lyb+TM4Zsn97Tifx2XmPeHR7ZwOgk4llzzVOUyYDmXuY8dy2qIzaarqcOFZtSBeLWVCed06YnX94Z5WcDFDaFPqKj0NqRs+9uCWdyIhO88R5PDVxEK4eBgwE9DJ4D/2bvpQm115w0n7k9cvFD0/u5fifJfe0Y8/ZTSUzNM4NeBp2rBFoA0jsDrzXhc00m1zHl4UECgYEA5bUVd5zlEtg25w9g0/N+EHdoBucXk5d09HGNtp9mPiA9T70g+driXK8czR8W+brvsCaYjjLmg2TKfbVbXLamj0xWQIhwZqRW51dNEanJKXJhgbWlY0wCf0yZp/m+bYzhu68Z7TI+LxRD1bIWNFzI8Of6/DaEdbE2I2tLhTCJTZUCgYEAw2AnRbZJa/oXUXnkxpvC4jxrQxgS2RGF9MXENJCAI7tM4GE5guIC++YhFv8D5nZeZY45QC4hbuE9ialXkau6uVqLriLXihQI42GnH9EBjEVH470MOf6A2gLLMjmO1kjKQHbMufAplw1Y+oAjbYwZnFW7wRHLr15MhQqKvguWGbECgYAoV7NbfIym0J5j2kmRL/R2A+KbQ77aRwFdZQwUhM46HwNlm7vM5epXiNGwHMO2PGSYNU8ZukrNzMfbaByRneqGxEtprgy/miFBJA3/CiiwRMxnMXXIiLLvlI5v9+a/6rxCcDBHfkl5jz+SqmJH8/u+g5+K6DA/U05EzjVHQQz8OQKBgAhvBSL4PHEhyZHlzh9Yp+/2JbcuudmO7RZk1xRhzHY+ZpIlAEOLGA/hnjoM5hEzuN1vZz9C/oR3yp0/px0NqbDInND2hhFazgtqsrkn34Y7k1/cUEPMnalLh5Pych0D5V8lAa9hE5qGo/mkQGNBMfXSqZkq+HzoeCsiCl0ryN3xAoGBANKOXhyGJ/v8YoX1jTwPwpRHnPI7uBgiq7m43aN/+pLz3PHFvnOTNIlpyniS0b1voIQIqveYWKhqLBnVZj5ahL9iAQaVR3ya/syUjMPWr8LBQna+7FSLrl3S0V5beJ6YXrWdnEGgzhHK/BaVnhdDXvnrzqXi5rGrjboXO9pncVU2-----END RSA PRIVATE KEY-----"),
		},
	}
	ctrl.SetControllerReference(m, secret, r.Scheme)
	return secret
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
	klog.Info("Checking if job is complete")
	return job.Status.Succeeded == 1
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mirroropenshiftiov1alpha1.Import{}).
		Complete(r)
}
