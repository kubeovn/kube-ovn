# Kube-OVN Code Style Guide

## Introduction

The goal of this guide is to manage the complexity, keep a consistent code style and prevent common mistakes. 
New code should follow the guides below and reviewers should check if new PRs follow the rules.

## Style

### Naming

Always use camelcase to name variables and functions.

<table>
<thead><tr><th>Bad</th><th>Good</th></tr></thead>
<tbody>
<tr><td>

```go
var command_line string
```

</td><td>

```go
var commandLine string
```

</td></tr>
</tbody></table>

### Error Handle

All error that not expected should be handled with error log. No error should be skipped silently.

<table>
<thead><tr><th>Bad</th><th>Good</th></tr></thead>
<tbody>
<tr><td>

```go
kubeClient, _ := kubernetes.NewForConfig(cfg)
```

</td><td>

```go
kubeClient, err := kubernetes.NewForConfig(cfg)
if err != nil {
    klog.Errorf("init kubernetes client failed %v", err)
    return err
}
```

</td></tr>
</tbody></table>

We prefer use `if err := somefunction(); err != nil {}` to check error in one line.

<table>
<thead><tr><th>Bad</th><th>Good</th></tr></thead>
<tbody>
<tr><td>

```go
err := c.initNodeRoutes()
if err != nil {
  klog.Fatalf("failed to initialize node routes: %v", err)
}
```

</td><td>

```go
if err := c.initNodeRoutes(); err != nil {
    klog.Fatalf("failed to initialize node routes: %v", err)
}
```

</td></tr>
</tbody></table>

### Function

The length of one function should not exceed 100 lines.

When err occurs in the function, it should be returned to the caller not skipped silently.

<table>
<thead><tr><th>Bad</th><th>Good</th></tr></thead>
<tbody>
<tr><td>

```go
func startHandle() {
	if err = some(); err != nil {
		klog.Errorf(err)    
    }
	return
}
```

</td><td>

```go
func startHandle() error {
    if err = some(); err != nil {
        klog.Errorf(err)
		return err
    }
    return nil
}
```

</td></tr>
</tbody></table>


## CRD

When adding a new CRD to Kube-OVN, you should consider things below to avoid common bugs.

1. The new feature should be disabled for performance and stability reasons.
2. The `install.sh`, `charts` and `yamls` should install the new CRD.
3. The `cleanup.sh` should clean the CRD and all the related resources.
4. The `gc.go` should check the inconsistent resource and do the cleanup.
5. The add/update/delete event can be triggered many times during the lifecycle, the handler should be reentrant.
