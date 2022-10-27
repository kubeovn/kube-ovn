package controller

import (
	"context"
	"os"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const ovnLeaderElector = "ovn-controller-leader-elector"

type leaderElectionConfig struct {
	PodName      string
	PodNamespace string

	Client clientset.Interface

	ElectionID string
	WasLeader  bool

	OnStartedLeading func(chan struct{})
	OnStoppedLeading func()
	OnNewLeader      func(identity string)
}

func (c *Controller) isLeader() bool {
	return c.elector.IsLeader()
}

func (c *Controller) leaderElection() {
	config := &leaderElectionConfig{
		Client:       c.config.KubeClient,
		ElectionID:   "kube-ovn-controller",
		PodName:      c.config.PodName,
		PodNamespace: c.config.PodNamespace,
	}
	c.elector = setupLeaderElection(config)

	var flag bool
	for {
		if c.isLeader() {
			config.WasLeader = true
			return
		}
		if !flag {
			klog.Info("waiting for becoming a leader")
			flag = true
		}
		time.Sleep(5 * time.Second)
	}
}

func setupLeaderElection(config *leaderElectionConfig) *leaderelection.LeaderElector {
	var stopCh chan struct{}
	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			klog.Infof("I am the new leader")
			stopCh = make(chan struct{})
			config.WasLeader = true

			if config.OnStartedLeading != nil {
				config.OnStartedLeading(stopCh)
			}
		},
		OnStoppedLeading: func() {
			klog.Info("I am not leader anymore")
			close(stopCh)

			if config.OnStoppedLeading != nil {
				config.OnStoppedLeading()
			}
			klog.Fatalf("leaderelection lost")
		},
		OnNewLeader: func(identity string) {
			klog.Infof("new leader elected: %v", identity)
			if config.WasLeader && identity != config.PodName {
				klog.Fatal("I am not leader anymore")
			}
			if config.OnNewLeader != nil {
				config.OnNewLeader(identity)
			}
		},
	}

	broadcaster := record.NewBroadcaster()
	hostname := os.Getenv(util.HostnameEnv)
	recorder := broadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{
		Component: ovnLeaderElector,
		Host:      hostname,
	})
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{Namespace: config.PodNamespace, Name: config.ElectionID},
		Client:    config.Client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      config.PodName,
			EventRecorder: recorder,
		},
	}
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks:     callbacks,
	})
	if err != nil {
		klog.Fatalf("unexpected error starting leader election: %v", err)
	}

	go elector.Run(context.Background())
	return elector
}
