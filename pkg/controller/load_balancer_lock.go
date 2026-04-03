package controller

// withLBKeyLock serializes controller-side read/modify/write operations for a
// single OVN load balancer. Callers should avoid nesting multiple LB locks at
// the same time; independent LBs can still be updated concurrently because the
// lock is scoped by lbName.
func (c *Controller) withLBKeyLock(lbName string, fn func() error) error {
	c.lbKeyMutex.LockKey(lbName)
	defer func() { _ = c.lbKeyMutex.UnlockKey(lbName) }()

	return fn()
}
