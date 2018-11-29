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
// logWriter just adds timestamps and a prefix to log and implemets the Writer interface
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
// This creates a channel that receieves randomly based on an exponential distribution
// returns a nil channel if kfi isn't enabled
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
			var checkpt checkpoint
			if len(backups) == 0 {
				checkpt.number = 1
			} else {
				checkpt.number = backups[len(backups)-1].number + 1
			}
			filename := fmt.Sprintf("%v.%v", checkpt.number, pid)
			checkpt.file = filename
			cmd := exec.Command("cr_checkpoint", "-f", filename, strconv.Itoa(pid))
			err := cmd.Run()
			if err == nil {
				log.Println("Created backup", checkpt.number)
				numBackups++
				backups = append(backups, checkpt)
				if len(backups) > *numCheckpoints {
					log.Println("Deleted backup", backups[0].number)
					os.Remove(backups[0].file)
					backups = backups[1:]
				}
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

type checkpoint struct {
	number, tries int
	file          string
}

var (
	backups             = make([]checkpoint, 0)
	kfiTimes, blcrTimes []time.Duration
	pid                 = -1
	numFaults           = 0
	numBackups          = 0
	numCheckpoints      *int
)

func interrupts(tout <-chan time.Time) {
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	select {
	case termSig := <-sig:
		log.Fatalf("Terminating early due to signal '%v'. Attempting to delete all checkpoints.\n", termSig)
	case <-tout:
		log.Println("Program timed out. terminating...")
	}
	for _, v := range backups {
		os.Remove(v.file)
	}

}
func main() {
	var (
		b backuper
		i injector
	)
	// command args
	log.SetFlags(log.Ltime)
	blcrEnabled := flag.Bool("blcr", false, "enable blcr")
	blcrRate := flag.Duration("b", time.Minute, "the time interval between blcr checkpoints (if enabled). Valid time units are 'ns', 'us' (or 'µs'), 'ms', 's', 'm', 'h'")
	kfiEnabled := flag.Bool("kfi", false, "enable kfi")
	kfiRate := flag.Duration("k", time.Minute, "average time between errors to be injected by kfi (if enabled). Valid time units are 'ns', 'us' (or 'µs'), 'ms', 's', 'm', 'h'")
	numCheckpoints = flag.Int("n", 3, "the number of blcr checkpoints to keep (if enabled)")
	retry := flag.Int("r", 3, "the number of times to retry a checkpoint before deleting it")
	timeout := flag.Duration("t", 0, "How long to wait before timing out (stopping the program)")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Println("Please supply a file to run")
		os.Exit(-1)
	}
	file := ""
	for i, v := range flag.Args() {
		file += v
		if i != len(flag.Args()) {
			file += " "
		}
	}
	if *blcrEnabled && *retry < 0 {
		fmt.Println("Error: retry count less than zero. You can't retry a checkpoint less than 0 times!")
		os.Exit(-1)
	}
	if *blcrEnabled && *numCheckpoints <= 0 {
		fmt.Println("Error: blcr was enabled but the number of checkpoints to keep was less than or equal to 0.")
		os.Exit(-1)
	}
	// have a goroutine ready to delete all backups if program times out or is interrupted
	if *timeout == 0 {
		go interrupts(nil)
	} else {
		go interrupts(time.After(*timeout))
	}

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
	// retry of there's backups left and more retries left
	for {
		if len(backups) < 1 {
			log.Printf("Command finished with an error (%v), no backups to restore from", err)
			break
		}
		backup := backups[len(backups)-1]
		if backup.tries >= *retry {
			log.Printf("Backup %v exceeded number of retries. Deleting it and trying a previous backup\n", backup.number)
			os.Remove(backup.file)
			backups = backups[:len(backups)-1]
			continue
		}
		backups[len(backups)-1].tries++
		log.Printf("Command finished with an error (%v), retrying from backup %v (retry #%v)\n", err, backup.number, backup.tries+1)
		cmd = exec.Command("cr_restart", "-f", backup.file)
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
	}
	log.Println("Failed to restore from backups. Now exiting.")
end:
	// delete all the backup files before returning
	for _, v := range backups {
		if err := os.Remove(v.file); err != nil {
			log.Printf("Error removing file '%v', please delete manually\n", v.file)
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
	fmt.Printf("Time spent checkpolinting (%v checkpoints): %v (%.2f%% of total execution)\n", numBackups, blcrTotal.Round(time.Millisecond), float64(blcrTotal)/float64(totalTime))
	fmt.Printf("Time spent injecting faults (%v faults): %v (%.2f%% of total execution)\n", numFaults, kfiTotal.Round(time.Millisecond), float64(kfiTotal)/float64(totalTime))
}
