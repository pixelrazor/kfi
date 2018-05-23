package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pixelrazor/kfi"

	"golang.org/x/exp/rand"

	"gonum.org/v1/gonum/stat/distuv"
)

type logWriter struct {
	prefix string
}

func (lw logWriter) Write(p []byte) (n int, err error) {
	for _, v := range strings.Split(strings.TrimSpace(string(p)), "\n") {
		log.Print(lw.prefix, v)
	}
	return len(p), nil
}

type injector struct {
	c, q chan struct{}
	rate time.Duration
}

func (i *injector) start() chan struct{} {
	if i.c == nil {
		return nil
	}
	i.q = make(chan struct{})
	go func() {
		dist := distuv.Exponential{
			1.0 / float64(i.rate.Nanoseconds()),
			rand.NewSource(uint64(time.Now().Unix())),
		}
		for {
			select {
			case <-time.After(time.Duration(dist.Rand()) * time.Nanosecond):
				i.c <- struct{}{}
			case <-i.q:
				return
			}
		}
	}()
	return i.c
}
func (i *injector) stop() {
	if i.c != nil {
		i.q <- struct{}{}
	}
}

type backuper struct {
	c    <-chan time.Time
	rate time.Duration
}

func run(cmd *exec.Cmd, b *backuper, i *injector) error {
	cmd.Start()
	if pid == -1 {
		pid = cmd.Process.Pid
	}
	bchan := b.c
	ichan := i.start()
	exit := make(chan error)
	go func() { exit <- cmd.Wait() }()
	for {
		select {
		case err := <-exit:
			close(exit)
			i.stop()
			log.Println("Command finshed")
			return err
		case <-bchan:
			b1 := time.Now()
			filename := fmt.Sprintf("%v.%v", len(backups)+1, pid)
			cmd := exec.Command("cr_checkpoint", "-f", filename, strconv.Itoa(pid))
			err := cmd.Run()
			if err == nil {
				log.Println("Created backup", len(backups)+1)
				numBackups++
				backups = append(backups, filename)
			} else {
				log.Println("error running cr_checkpoint:", err)
			}
			blcrTimes = append(blcrTimes, time.Since(b1))
			bchan = time.After(b.rate)
		case <-ichan:
			k1 := time.Now()
			res, err := kfi.InjectByInt(pid)
			if err != nil {
				log.Println("kfi.Inject:", err)
			} else {
				log.Println(res)
				numFaults++
			}
			kfiTimes = append(kfiTimes, time.Since(k1))
		}
	}
}

var (
	backups             = make([]string, 0)
	kfiTimes, blcrTimes []time.Duration
	pid                 = -1
	numFaults           = 0
	numBackups          = 0
)

func interrupts() {
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	for _, v := range backups {
		os.Remove(v)
	}
	log.Fatalf("Terminating early due to signal '%v'. Attempted to delete all checkpoints.\n", <-sig)
}
func main() {
	var (
		b backuper
		i injector
	)
	log.SetFlags(log.Ltime)
	blcrEnabled := flag.Bool("blcr", false, "enable blcr")
	blcrRate := flag.Duration("b", time.Minute, "the time interval between blcr checkpoints (if enabled). Valid time units are 'ns', 'us' (or 'µs'), 'ms', 's', 'm', 'h'")
	kfiEnabled := flag.Bool("kfi", false, "enable kfi")
	kfiRate := flag.Duration("k", time.Minute, "average time between errors to be injected by kfi (if enabled). Valid time units are 'ns', 'us' (or 'µs'), 'ms', 's', 'm', 'h'")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Println("Please supply a file to run")
		os.Exit(-1)
	}
	go interrupts()
	file := flag.Arg(0)
	if *kfiEnabled {
		kfiTimes = make([]time.Duration, 0)
		i.c = make(chan struct{})
		i.rate = *kfiRate
	}
	if *blcrEnabled {
		blcrTimes = make([]time.Duration, 0)
		b.c = time.After(*blcrRate)
		b.rate = *blcrRate
	}
	// load the cr library and run the given program
	cmd := exec.Command("bash", "-c", "env LD_PRELOAD=/usr/local/lib/libcr_run.so.0 "+file)

	cmd.Stdout = logWriter{"stdout: "}
	cmd.Stderr = logWriter{"stderr: "}

	startTime := time.Now()

	err := run(cmd, &b, &i)

	if err == nil {
		log.Println("Program finished running and exited cleanly.")
		goto end
	}
	for {
		backupNum := len(backups)
		log.Printf("Command finished with an error (%v), retrying from backup #%v\n", err, backupNum)
		if len(backups) < 1 {
			log.Println("No backups to restore from")
			break
		}
		cmd = exec.Command("cr_restart", "-f", backups[backupNum-1])
		cmd.Stdout = logWriter{"stdout: "}
		cmd.Stderr = logWriter{"stderr: "}
		if *kfiEnabled {
			kfiTimes = make([]time.Duration, 0)
			i.c = make(chan struct{})
			i.rate = *kfiRate
		}
		if *blcrEnabled {
			blcrTimes = make([]time.Duration, 0)
			b.c = time.After(*blcrRate)
			b.rate = *blcrRate
		}
		err = run(cmd, &b, &i)
		if err == nil {
			log.Println("Program finished running and exited cleanly.")
			goto end
		}
		for _, v := range backups[backupNum-1:] {
			if err := os.Remove(v); err != nil {
				log.Printf("Error removing file '%v', please delete manually\n", v)
			}
		}
		if len(backups) == backupNum {
			backups = backups[:backupNum-1]
		}

	}
	log.Println("Failed to restore from backups. Now exiting.")
end:
	// delete all the backup files before returning
	for _, v := range backups {
		if err := os.Remove(v); err != nil {
			log.Printf("Error removing file '%v', please delete manually\n", v)
		}
	}
	totalTime := time.Since(startTime)
	fmt.Println("Total execution time:", totalTime.Round(time.Millisecond))
	var blcrTotal, kfiTotal time.Duration
	for _, v := range blcrTimes {
		blcrTotal += v
	}
	for _, v := range kfiTimes {
		kfiTotal += v
	}
	fmt.Println("Time spent running the process:", (totalTime - kfiTotal - blcrTotal).Round(time.Millisecond))
	fmt.Printf("Time spent checkpointing (%v checkpoints): %v (%.2f%% of total execution)\n", numBackups, blcrTotal.Round(time.Millisecond), float64(blcrTotal)/float64(totalTime))
	fmt.Printf("Time spent injectiong faults (%v faults): %v (%.2f%% of total execution)\n", numFaults, kfiTotal.Round(time.Millisecond), float64(kfiTotal)/float64(totalTime))
}
