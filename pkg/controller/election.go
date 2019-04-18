package controller

import (
	"context"
	"os"
	"time"

	"k8s.io/klog"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
)

const ovnLeaderElector = "ovn-controller-leader-elector"

type leaderElectionConfig struct {
	PodName      string
	PodNamespace string

	Client clientset.Interface

	ElectionID string

	OnStartedLeading func(chan struct{})
	OnStoppedLeading func()
	OnNewLeader      func(identity string)
}

func setupLeaderElection(config *leaderElectionConfig) {
	var elector *leaderelection.LeaderElector

	// start a new context
	ctx := context.Background()

	var cancelContext context.CancelFunc

	var newLeaderCtx = func(ctx context.Context) context.CancelFunc {
		// allow to cancel the context in case we stop being the leader
		leaderCtx, cancel := context.WithCancel(ctx)
		go elector.Run(leaderCtx)
		return cancel
	}

	var stopCh chan struct{}
	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			klog.Infof("I am the new leader")
			stopCh = make(chan struct{})

			if config.OnStartedLeading != nil {
				config.OnStartedLeading(stopCh)
			}
		},
		OnStoppedLeading: func() {
			klog.Info("I am not leader anymore")
			close(stopCh)

			// cancel the context
			cancelContext()

			cancelContext = newLeaderCtx(ctx)

			if config.OnStoppedLeading != nil {
				config.OnStoppedLeading()
			}
		},
		OnNewLeader: func(identity string) {
			klog.Infof("new leader elected: %v", identity)
			if config.OnNewLeader != nil {
				config.OnNewLeader(identity)
			}
		},
	}

	broadcaster := record.NewBroadcaster()
	hostname, _ := os.Hostname()

	recorder := broadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{
		Component: ovnLeaderElector,
		Host:      hostname,
	})

	lock := resourcelock.ConfigMapLock{
		ConfigMapMeta: metav1.ObjectMeta{Namespace: config.PodNamespace, Name: config.ElectionID},
		Client:        config.Client.CoreV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      config.PodName,
			EventRecorder: recorder,
		},
	}

	ttl := 30 * time.Second
	var err error

	elector, err = leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          &lock,
		LeaseDuration: ttl,
		RenewDeadline: ttl / 2,
		RetryPeriod:   ttl / 4,

		Callbacks: callbacks,
	})
	if err != nil {
		klog.Fatalf("unexpected error starting leader election: %v", err)
	}

	cancelContext = newLeaderCtx(ctx)
}
