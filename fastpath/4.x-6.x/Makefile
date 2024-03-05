MODULE_NAME:=kube_ovn_fastpath
ifneq ($(KERNELRELEASE),)
mymodule-objs:=${MODULE_NAME}.o
obj-m:=${MODULE_NAME}.o
else
PWD:=$(shell pwd)
KVER:=$(shell uname -r)


KDIR:=/lib/modules/$(shell uname -r)/build
all:
	$(MAKE) -C $(KDIR) M=$(PWD)
#clean:
	@rm -rf .*.com *.o *.mod.c  .tmp_versions modules.order Module.symvers
install:
	echo ${KDIR}
	@insmod ${MODULE_NAME}.ko
uninstall:
	@rmmod ${MODULE_NAME}.ko
endif
