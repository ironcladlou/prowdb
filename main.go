package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logging "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"
)

func init() {
	logging.SetLogger(zap.New())
}

func main() {
	var cmd = &cobra.Command{Use: "ez-thanos-operator"}
	cmd.AddCommand(NewStartCommand())

	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}

type Thanos struct {
	metav1.ObjectMeta
	Spec ThanosSpec
}
type ThanosSpec struct {
	URLs []string `json:"urls"`
}

type Operator struct {
	FetcherImage    string
	PrometheusImage string
	ThanosImage     string
	Namespace       string

	log    logr.Logger
	client client.Client
}

func NewStartCommand() *cobra.Command {
	var operator Operator

	var command = &cobra.Command{
		Use:   "start",
		Short: "Starts the operator.",
		Run: func(cmd *cobra.Command, args []string) {
			mgr, err := manager.New(clientconfig.GetConfigOrDie(), manager.Options{
				Namespace:          operator.Namespace,
				MetricsBindAddress: "0",
			})
			if err != nil {
				panic(err)
			}
			err = routev1.Install(mgr.GetScheme())
			if err != nil {
				panic(err)
			}
			operator.log = logging.Log.WithName("operator")
			operator.client = mgr.GetClient()
			if err := operator.Start(mgr); err != nil {
				panic(err)
			}
		},
	}

	command.Flags().StringVarP(&operator.FetcherImage, "fetcher-image", "", "quay.io/fedora/fedora:31-x86_64", "")
	command.Flags().StringVarP(&operator.PrometheusImage, "prometheus-image", "", "quay.io/prometheus/prometheus:v2.17.2", "")
	command.Flags().StringVarP(&operator.ThanosImage, "thanos-image", "", "quay.io/thanos/thanos:v0.14.0", "")
	command.Flags().StringVarP(&operator.Namespace, "namespace", "", "ez-thanos-operator", "")

	return command
}

func (o Operator) Start(mgr manager.Manager) error {
	log := o.log.WithName("entrypoint")

	// Setup a new controller to reconcile ConfigMaps
	c, err := controller.New("configmap-controller", mgr, controller.Options{
		Reconciler: reconcile.Func(func(request reconcile.Request) (reconcile.Result, error) {
			return o.reconcileConfigMap(request)
		}),
	})
	if err != nil {
		return fmt.Errorf("unable to set up configmap controller: %w", err)
	}

	// Watch ConfigMaps and enqueue ConfigMap object key
	if err := c.Watch(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return fmt.Errorf("unable to watch configmaps: %w", err)
	}

	log.Info("starting operator")
	return mgr.Start(signals.SetupSignalHandler())
}

func (o Operator) reconcileConfigMap(request reconcile.Request) (reconcile.Result, error) {
	log := o.log.WithValues("request", request)

	cm := &corev1.ConfigMap{}
	err := o.client.Get(context.TODO(), request.NamespacedName, cm)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(nil, "couldn't find configmap")
			// TODO: update deployment to remove configmap reference
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("couldn't fetch configmap: %w", err)
	}

	log.Info("reconciling configmap", "name", cm.Name)

	data, hasData := cm.Data["thanos.yaml"]
	if !hasData {
		log.Error(nil, "configmap is missing thanos.yaml key")
		return reconcile.Result{}, nil
	}

	thanos := Thanos{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cm.Namespace,
			Name:      cm.Name,
		},
	}
	err = yaml.Unmarshal([]byte(data), &thanos.Spec)
	if err != nil {
		log.Error(err, "configmap has invalid thanos.yaml contents")
		return reconcile.Result{}, nil
	}

	log.Info("observing configmap", "name", cm.Name, "spec", thanos.Spec)

	var wg sync.WaitGroup
	createErrors := []error{}
	errorChannel := make(chan error)
	defer close(errorChannel)
	go func() {
		for err := range errorChannel {
			createErrors = append(createErrors, err)
		}
	}()
	for i := range thanos.Spec.URLs {
		url := thanos.Spec.URLs[i]
		prometheusDeploymentName := o.prometheusDeploymentName(url)
		prometheusDeployment := &appsv1.Deployment{}
		hasPrometheusDeployment := true
		err := o.client.Get(context.TODO(), prometheusDeploymentName, prometheusDeployment)
		if err != nil {
			if errors.IsNotFound(err) {
				hasPrometheusDeployment = false
			} else {
				return reconcile.Result{}, fmt.Errorf("couldn't fetch deployment: %w", err)
			}
		}
		if !hasPrometheusDeployment {
			wg.Add(1)
			go func() {
				defer wg.Done()
				log.Info("started fetching prow info", "url", url)
				prowInfo, err := getProwInfo(url)
				if err != nil {
					errorChannel <- fmt.Errorf("couldn't get prow info for %s: %w", url, err)
					return
				}
				log.Info("finished fetching prow info", "url", url)
				prometheusDeployment = o.prometheusDeploymentManifest(url, prowInfo.MetricsURL, prowInfo.Started, prowInfo.Finished)
				prometheusDeployment.Spec.Template.Labels[thanos.Name] = "true"
				err = o.client.Create(context.TODO(), prometheusDeployment)
				if err != nil {
					errorChannel <- fmt.Errorf("couldn't create deployment for url %s: %w", url, err)
					return
				} else {
					log.Info("created deployment", "name", prometheusDeployment.Name, "url", url, "started", prowInfo.Started, "finished", prowInfo.Finished)
				}
			}()
		} else {
			_, hasReference := prometheusDeployment.Spec.Template.Labels[thanos.Name]
			if !hasReference {
				prometheusDeployment.Spec.Template.Labels[thanos.Name] = "true"
				err := o.client.Update(context.TODO(), prometheusDeployment)
				if err != nil {
					return reconcile.Result{}, fmt.Errorf("couldn't update deployment for url %s: %w", url, err)
				} else {
					log.Info("updated deployment", "name", prometheusDeployment.Name, "url", url)
				}
			}
		}
	}
	wg.Wait()
	for _, err := range createErrors {
		log.Error(err, "failed to create deployment")
	}

	storeService := &corev1.Service{}
	storeServiceName := o.thanosStoreServiceName(thanos)
	hasStoreService := true
	err = o.client.Get(context.TODO(), storeServiceName, storeService)
	if err != nil {
		if errors.IsNotFound(err) {
			hasStoreService = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch service: %w", err)
		}
	}
	if !hasStoreService {
		storeService = o.thanosStoreServiceManifest(thanos)
		err = o.client.Create(context.TODO(), storeService)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create service: %w", err)
		} else {
			log.Info("created service", "name", storeService.Name)
		}
	}

	queryDeployment := &appsv1.Deployment{}
	queryDeploymentName := o.thanosQueryDeploymentName(thanos)
	hasQueryDeployment := true
	err = o.client.Get(context.TODO(), queryDeploymentName, queryDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			hasQueryDeployment = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch deployment: %w", err)
		}
	}
	if !hasQueryDeployment {
		queryDeployment = o.thanosQueryDeploymentManifest(thanos)
		err = o.client.Create(context.TODO(), queryDeployment)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create deployment: %w", err)
		} else {
			log.Info("created deployment", "name", queryDeployment.Name)
		}
	}

	queryService := &corev1.Service{}
	queryServiceName := o.thanosQueryServiceName(thanos)
	hasQueryService := true
	err = o.client.Get(context.TODO(), queryServiceName, queryService)
	if err != nil {
		if errors.IsNotFound(err) {
			hasQueryService = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch service: %w", err)
		}
	}
	if !hasQueryService {
		queryService = o.thanosQueryServiceManifest(thanos)
		err = o.client.Create(context.TODO(), queryService)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create service: %w", err)
		} else {
			log.Info("created service", "name", queryService.Name)
		}
	}

	queryRoute := &routev1.Route{}
	queryRouteName := o.thanosQueryRouteName(thanos)
	hasQueryRoute := true
	err = o.client.Get(context.TODO(), queryRouteName, queryRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			hasQueryRoute = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch route: %w", err)
		}
	}
	if !hasQueryRoute {
		queryRoute = o.thanosQueryRouteManifest(thanos)
		err = o.client.Create(context.TODO(), queryRoute)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create route: %w", err)
		} else {
			log.Info("created route", "name", queryRoute.Name)
		}
	}

	return reconcile.Result{}, nil
}

func (o Operator) prometheusDeploymentName(url string) types.NamespacedName {
	hash := sha256.Sum256([]byte(url))
	name := fmt.Sprintf("prometheus-%x", hash[:6])
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o Operator) prometheusDeploymentManifest(url string, metricsURL string, started, finished time.Time) *appsv1.Deployment {
	name := o.prometheusDeploymentName(url)
	sharePIDNamespace := true
	var replicas int32 = 1

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
			Labels: map[string]string{
				"app": "prometheus",
			},
			Annotations: map[string]string{
				"url":      url,
				"started":  started.String(),
				"finished": finished.String(),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":        "prometheus",
					"prometheus": name.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":        "prometheus",
						"prometheus": name.Name,
					},
					Annotations: map[string]string{
						"url":      url,
						"started":  started.String(),
						"finished": finished.String(),
					},
				},
				Spec: corev1.PodSpec{
					ShareProcessNamespace: &sharePIDNamespace,
					Volumes: []corev1.Volume{
						{
							Name: "prometheus-storage-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:       "setup",
							Image:      o.FetcherImage,
							Command:    []string{"/bin/bash", "-c", deploymentInitScript(name.Name)},
							WorkingDir: "/prometheus/",
							Env: []corev1.EnvVar{
								{
									Name:  "PROMTAR",
									Value: metricsURL,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "prometheus",
							Command: []string{
								"/bin/prometheus",
								"--storage.tsdb.max-block-duration=2h",
								"--storage.tsdb.min-block-duration=2h",
								"--web.enable-lifecycle",
								"--storage.tsdb.path=/prometheus",
								"--config.file=/prometheus/prometheus.yml",
							},
							Image: o.PrometheusImage,
							Ports: []corev1.ContainerPort{
								{
									Name:          "webui",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 9090,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse("100m"),
									"memory": resource.MustParse("500Mi"),
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds:   1,
								PeriodSeconds:    10,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromInt(9090),
										Scheme: "HTTP",
									},
								},
							},
						},
						{
							Name: "thanos-sidecar",
							Command: []string{
								"/bin/thanos",
								"sidecar",
								"--tsdb.path=/prometheus",
								"--prometheus.url=http://localhost:9090",
								"--shipper.upload-compacted",
							},
							Image: o.ThanosImage,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse("100m"),
									"memory": resource.MustParse("500Mi"),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "webui",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 9090,
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds:   1,
								PeriodSeconds:    10,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromInt(9090),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (o Operator) thanosStoreServiceName(thanos Thanos) types.NamespacedName {
	name := fmt.Sprintf("store-%s", thanos.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o Operator) thanosStoreServiceManifest(thanos Thanos) *corev1.Service {
	name := o.thanosStoreServiceName(thanos)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{
				{
					Name:     "grpc",
					Port:     10901,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "http",
					Port:     10902,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app":       "prometheus",
				thanos.Name: "true",
			},
		},
	}
}

func (o Operator) thanosQueryDeploymentName(thanos Thanos) types.NamespacedName {
	name := fmt.Sprintf("query-%s", thanos.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o Operator) thanosQueryDeploymentManifest(thanos Thanos) *appsv1.Deployment {
	name := o.thanosQueryDeploymentName(thanos)
	storeServiceName := o.thanosStoreServiceName(thanos)
	var replicas int32 = 1
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
			Labels: map[string]string{
				"app": "thanos-query",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":    "thanos-query",
					"thanos": thanos.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":    "thanos-query",
						"thanos": thanos.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "query",
							Image: o.ThanosImage,
							Command: []string{
								"/bin/thanos",
								"query",
								"--http-address=0.0.0.0:19192",
								"--store.sd-dns-interval=10s",
								fmt.Sprintf("--store=dnssrv+_grpc._tcp.%s.%s.svc", storeServiceName.Name, storeServiceName.Namespace),
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 19192,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse("100m"),
									"memory": resource.MustParse("500Mi"),
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds:   1,
								PeriodSeconds:    10,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromInt(19192),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (o Operator) thanosQueryServiceName(thanos Thanos) types.NamespacedName {
	name := fmt.Sprintf("query-%s", thanos.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o Operator) thanosQueryServiceManifest(thanos Thanos) *corev1.Service {
	name := o.thanosQueryServiceName(thanos)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     19192,
					Protocol: corev1.ProtocolTCP,
					Name:     "http",
				},
				{
					Port:     10901,
					Protocol: corev1.ProtocolTCP,
					Name:     "grpc",
				},
			},
			Selector: map[string]string{
				"app":    "thanos-query",
				"thanos": thanos.Name,
			},
		},
	}
}

func (o Operator) thanosQueryRouteName(thanos Thanos) types.NamespacedName {
	name := fmt.Sprintf("query-%s", thanos.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o Operator) thanosQueryRouteManifest(thanos Thanos) *routev1.Route {
	name := o.thanosQueryRouteName(thanos)
	queryServiceName := o.thanosQueryServiceName(thanos)
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: queryServiceName.Name,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http"),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}
}

func deploymentInitScript(name string) string {
	return fmt.Sprintf(`set -uxo pipefail
umask 0000
curl -sL ${PROMTAR} | tar xvz -m
chown -R 65534:65534 /prometheus

cat >/prometheus/prometheus.yml <<EOL
# my global config
global:
  external_labels:
    cluster_name: %s

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
    - targets: ['localhost:9090']
EOL
`, name)
}
