package util

import (
	"net"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

func TestGatewayOnLinkRoutes(t *testing.T) {
	routes, err := GatewayOnLinkRoutes("10.0.0.1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}
	if routes[0].Dst.String() != "10.0.0.1/32" {
		t.Fatalf("got dst %s, want 10.0.0.1/32", routes[0].Dst)
	}

	routes, err = GatewayOnLinkRoutes("fd00::1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if routes[0].Dst.String() != "fd00::1/128" {
		t.Fatalf("got dst %s, want fd00::1/128", routes[0].Dst)
	}

	routes, err = GatewayOnLinkRoutes("10.0.0.1,fd00::1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}
	if !routes[0].Gateway.Equal(net.ParseIP("10.0.0.1")) || !routes[1].Gateway.Equal(net.ParseIP("fd00::1")) {
		t.Fatalf("unexpected gateways: %#v", routes)
	}

	if _, err := GatewayOnLinkRoutes("not-an-ip", 1); err == nil {
		t.Fatal("expected error for invalid gateway")
	}
}

func TestValidateRoutedAnnotationRoutes(t *testing.T) {
	if err := ValidateRoutedAnnotationRoutes(nil, "10.0.0.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ok := []request.Route{{Destination: "192.168.1.0/24", Gateway: "10.0.0.1"}}
	if err := ValidateRoutedAnnotationRoutes(ok, "10.0.0.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateRoutedAnnotationRoutes(ok, "10.0.0.1,fd00::1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ValidateRoutedAnnotationRoutes([]request.Route{{Destination: "192.168.1.0/24", Gateway: "10.0.0.2"}}, "10.0.0.1"); err == nil {
		t.Fatal("expected error for non-subnet gateway")
	}
	if err := ValidateRoutedAnnotationRoutes([]request.Route{{Destination: "192.168.1.0/24"}}, "10.0.0.1"); err == nil {
		t.Fatal("expected error for empty gateway")
	}
	if err := ValidateRoutedAnnotationRoutes([]request.Route{{Destination: "bad", Gateway: "10.0.0.1"}}, "10.0.0.1"); err == nil {
		t.Fatal("expected error for invalid destination")
	}
}
