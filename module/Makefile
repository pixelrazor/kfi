obj-m += kfi.o

all:
	make -C /lib/modules/$(shell uname -r)/build M=$$PWD modules

install:
	make -C /lib/modules/$(shell uname -r)/build M=$$PWD modules_install
	depmod -ae

clean:
	make -C /lib/modules/$(shell uname -r)/build M=$$PWD clean

remove:
	sudo rmmod kfi

insert:
	sudo insmod kfi.ko


