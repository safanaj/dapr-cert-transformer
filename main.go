package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	goflag "flag"
	flag "github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	progname    string = "dapr-cert-transformer"
	version     string
	showVersion bool = false

	daprTrustBundleNamespace string = ""
	daprTrustBundleName      string = "dapr-trust-bundle"
	certSourceKey            string = "tls.crt"
	certDestKey              string = "issuer.crt"
	keySourceKey             string = "tls.key"
	keyDestKey               string = "issuer.key"
)

func parseFlags() {
	flag.BoolVar(&showVersion, "version", showVersion, "Print version and exit")
	flag.StringVar(&daprTrustBundleNamespace, "watch-secret-namespace", daprTrustBundleNamespace, "")
	flag.StringVar(&daprTrustBundleName, "watch-secret-name", daprTrustBundleName, "")
	flag.StringVar(&certSourceKey, "source-cert-secret-key", certSourceKey, "")
	flag.StringVar(&certDestKey, "dest-cert-secret-key", certDestKey, "")
	flag.StringVar(&keySourceKey, "source-key-secret-key", keySourceKey, "")
	flag.StringVar(&keyDestKey, "dest-key-secret-key", keyDestKey, "")
	flag.CommandLine.AddGoFlag(goflag.Lookup("kubeconfig"))
	flag.Parse()
}

func isDaprTrustBundleSecret(o client.ObjectKey) bool {
	return o.Namespace == daprTrustBundleNamespace && o.Name == daprTrustBundleName
}

func needsDaprTrustBundleSecretUpdate(s *corev1.Secret) bool {
	tlsCrt, tlsCrtOk := s.Data[certSourceKey]
	tlsKey, tlsKeyOk := s.Data[keySourceKey]
	issuerCrt, _ := s.Data[certDestKey]
	issuerKey, _ := s.Data[keyDestKey]
	return (tlsCrtOk && tlsKeyOk && (string(tlsCrt) != string(issuerCrt) || string(tlsKey) != string(issuerKey)))
}

func main() {
	parseFlags()

	if showVersion {
		fmt.Printf("%s version %s\n", progname, version)
		os.Exit(0)
	}

	logf.SetLogger(zap.New())

	log := logf.Log.WithName(progname)

	if daprTrustBundleNamespace == "" {
		podNs := os.Getenv("POD_NAMESPACE")
		if podNs == "" {
			log.Error(fmt.Errorf("Missing POD_NAMESPACE environment variable"), "could not identify namespace to watch")
			os.Exit(1)
		}
		daprTrustBundleNamespace = podNs
	}

	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{Namespace: daprTrustBundleNamespace, Logger: log.WithName("mgr")})
	if err != nil {
		log.Error(err, "could not create manager")
		os.Exit(1)
	}

	mgr.AddHealthzCheck("ping", healthz.Ping)
	mgr.AddReadyzCheck("ready", func(_ *http.Request) error {
		i, err := mgr.GetCache().GetInformer(context.TODO(), &corev1.Secret{})
		if err != nil {
			return err
		}
		if !i.HasSynced() {
			return fmt.Errorf("Secret informer not in sync")
		}
		return nil
	})

	// cl := mgr.GetClient()
	err = builder.
		ControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		WithEventFilter(predicate.And(predicate.ResourceVersionChangedPredicate{}, predicate.Funcs{
			CreateFunc: func(evt event.CreateEvent) bool {
				return isDaprTrustBundleSecret(client.ObjectKeyFromObject(evt.Object))
			},
			UpdateFunc: func(evt event.UpdateEvent) bool {
				return isDaprTrustBundleSecret(client.ObjectKeyFromObject(evt.ObjectNew))
			},
		})).
		Complete(&DaprSecretReconciler{})

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "could not start manager")
		os.Exit(1)
	}
}

// ReplicaSetReconciler is a simple ControllerManagedBy example implementation.
type DaprSecretReconciler struct {
	client.Client
}

func (a *DaprSecretReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	l := logf.FromContext(ctx)

	l.V(4).Info("DaprSecretReconciler.Reconcile ...")
	if !isDaprTrustBundleSecret(req.NamespacedName) {
		l.Info("Avoid reconciling other secret")
		return reconcile.Result{}, nil
	}

	s := &corev1.Secret{}
	err := a.Get(ctx, req.NamespacedName, s)
	if err != nil {
		l.Error(err, "Failed to get secret")
		return reconcile.Result{}, err
	}

	if !needsDaprTrustBundleSecretUpdate(s) {
		l.Info("Already up-to-date")
		return reconcile.Result{}, nil
	}

	s.Data[certDestKey] = s.Data[certSourceKey]
	s.Data[keyDestKey] = s.Data[keySourceKey]

	err = a.Update(ctx, s)
	if err != nil {
		l.Error(err, "Failed to update secret")
		return reconcile.Result{}, fmt.Errorf("could not update Secret: %+v", err)
	}

	l.Info("Successful reconciliation")
	return reconcile.Result{}, nil
}

func (a *DaprSecretReconciler) InjectClient(c client.Client) error {
	a.Client = c
	return nil
}
