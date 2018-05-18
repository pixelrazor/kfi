# Kernel-module Fault Injector (KFI)
KFI is a kernel module that injects faults into user process.
The module directory contains the module and the makefile, the base diretory contains a Go library for using it, and kfi-inject is a command line tool for injecting faults.
It currently follows these steps:
1. Find the process's pid and task structs
2. Send SIGSTOP to the process
3. Sleep until the process enters stopped state
4. Pick 2 random numbers
5. Use the random numbers to pick a register to inject and which bit to flip
6. Send SIGCONT to the process
## Building
The module was built and tested with kernel verion 3.4.112.
It should be pretty portable, the only major thing that might change between versions is the pt\_regs struct.
If it doesn't immediatly work for your version, just change the values of regs\_len and reg\_names to the appropriate values for your kernel (These can be found in the arch/x86/include/asm/ptrace.h file of your kernel's source)
1. `make`
2. insert (`make insert` will insert it with insmod. You could also use `make install` if you would like to use modprobe)
If you wish to build the cli tool, run `go get github.com/pixelrazor/kfi/kfi-inject`
NOTE: this requires you have the go binaries installed, and will place the binary in $GOPATH/bin
## Support me
<a href="https://www.buymeacoffee.com/iZ1Dhem" target="_blank"><img src="https://www.buymeacoffee.com/assets/img/custom_images/purple_img.png" alt="Buy Me A Coffee" style="height: auto !important;width: auto !important;" ></a>