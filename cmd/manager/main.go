package main

import (
	"flag"
	"os"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/portworx/kubevirt-observability-operator/controllers"
	"github.com/portworx/kubevirt-observability-operator/webhook"
	routev1 "github.com/openshift/api/route/v1"
)

func main() {
	var enableLeaderElection bool

	opts := ctrlzap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election")
	flag.Parse()

	ctrl.SetLogger(ctrlzap.New(ctrlzap.UseFlagOptions(&opts)))

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kubevirtv1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: ":8080",
		},
		HealthProbeBindAddress: ":8081",
		WebhookServer: crwebhook.NewServer(crwebhook.Options{
			Port: 9443,
		}),
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "vm-observability-operator.kubevirt-observability.io",
	})
	if err != nil {
		os.Exit(1)
	}

	if err := (&controllers.VMReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("kubevirt-observability-operator"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}

	decoder := admission.NewDecoder(mgr.GetScheme())
	mutator := &webhook.VMMutator{
		Decoder: decoder,
	}
	server := mgr.GetWebhookServer()
	server.Register("/mutate-kubevirt-io-v1-virtualmachine", &admission.Webhook{Handler: mutator})

	_ = mgr.AddHealthzCheck("healthz", healthz.Ping)
	_ = mgr.AddReadyzCheck("readyz", healthz.Ping)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		os.Exit(1)
	}
}
