package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var Version = "development"
var BuildDate = "NAN"
var baseMem = int64(4000)
var basePath = "/usr/share/sdfs"
var running bool

type Subsystem struct {
	XMLName xml.Name `xml:"subsystem-config"`
	Io      Io       `xml:"io"`
}

type Io struct {
	XMLName         xml.Name `xml:"io"`
	MaxOpenFiles    int      `xml:"max-open-files,attr"`
	MaxWriteBuffers int      `xml:"max-file-write-buffers,attr"`
}

func main() {
	volume := flag.String("v", "", "Volume to mount.")
	volumeFile := flag.String("f", "", "sdfs volume configuration file to mount \ne.g. /etc/sdfs/dedup-volume-cfg.xml")
	memory := flag.Int64("z", 0, "quiet")
	version := flag.Bool("version", false, "The Version of this build")
	flag.String("o", "direct_io,big_writes,allow_other,fsname=SDFS", "fuse mount options.\nWill default to: \ndirect_io,big_writes,allow_other,fsname=SDFS")
	flag.Bool("r", false, "Restores files from cloud storage if the backend cloud store supports it")
	flag.Bool("d", false, "debug output")
	flag.Int("p", 6442, "port to use for sdfs cli")
	flag.Bool("l", false, "Compact Volume on Disk")
	flag.Bool("c", false, "Runs Consistency Check")
	flag.String("e", "", "password to decrypt config")
	flag.Bool("n", false, "disable drive mount")
	flag.String("m", "", "mount point for SDFS file system \n e.g. /media/dedup")
	flag.Bool("h", false, "displays available options")
	flag.Bool("s", false, "If set ssl will not be used sdfscli traffic.")
	flag.Bool("w", false, "Sync With All Files in Cloud.")
	flag.Bool("q", false, "Use Console Logging.")
	flag.Parse()
	if *version {
		fmt.Printf("Version : %s\n", Version)
		fmt.Printf("Build Date: %s\n", BuildDate)
		os.Exit(0)
	}

	if isFlagPassed("h") {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])

		flag.PrintDefaults()
		os.Exit(1)
	}
	if !isFlagPassed("v") && !isFlagPassed("f") {
		log.Fatalf("-v or -f must be passed with the name of the volume")
	}
	xmlFilePath := *volumeFile
	if isFlagPassed("v") {
		xmlFilePath = fmt.Sprintf("/etc/sdfs/%s-volume-cfg.xml", *volume)
	}
	if _, err := os.Stat(xmlFilePath); os.IsNotExist(err) {
		fmt.Printf("File %s does not exist", xmlFilePath)
		os.Exit(1)
	}
	if len(os.Getenv("SDFS_BASE_PATH")) > 0 {
		basePath = os.Getenv("SDFS_BASE_PATH")
	}
	if !isFlagPassed("z") {
		xmlFile, err := os.Open(xmlFilePath)
		// if we os.Open returns an error then handle it
		if err != nil {
			fmt.Printf("Error Reading %s : %v\n", xmlFilePath, err)
			os.Exit(1)
		}
		byteValue, _ := ioutil.ReadAll(xmlFile)

		var subsystem Subsystem
		xml.Unmarshal(byteValue, &subsystem)
		if subsystem.Io.MaxWriteBuffers > 80 {
			subsystem.Io.MaxWriteBuffers = 80
		}
		cm := int64(subsystem.Io.MaxWriteBuffers) * int64(subsystem.Io.MaxOpenFiles) * int64(2)
		*memory = baseMem + cm

	}
	pf := fmt.Sprintf("%s.pid", *volume)
	memString := fmt.Sprintf("%dM", *memory)
	var execPath = fmt.Sprintf("%s/jsvc", basePath)
	cmdStr := " -server -outfile &1 -errfile &2 " +
		"-Djava.library.path=" + basePath + "/bin/ -home " + basePath + "/bin/jre -Dorg.apache.commons.logging.Log=fuse.logging.FuseLog -Xss2m " +
		"-wait 99999999999 -Dfuse.logging.level=INFO -Dfile.encoding=UTF-8 " + os.Getenv("DOCKER_DETATCH") + " -Xmx" + memString + " -Xms" + memString + " " +
		"-XX:+DisableExplicitGC -pidfile /tmp/" + pf + " -XX:+UseG1GC -Djava.awt.headless=true " +
		"-cp " + basePath + "/lib/sdfs.jar:" + basePath + "/lib/* fuse.SDFS.MountSDFS " + strings.Join(os.Args[1:], " ")
	if isFlagPassed("d") {
		fmt.Printf("%s %s", execPath, cmdStr)
	}
	cmd := exec.Command(execPath, strings.Fields(cmdStr)...)
	stdoutIn, _ := cmd.StdoutPipe()
	stderrIn, _ := cmd.StderrPipe()
	err := cmd.Start()
	if err != nil {
		log.Printf("failed with '%v'\n", err)
		log.Fatalf("command: %s %s\n", execPath, cmdStr)

	}

	// cmd.Wait() should be called only after we finish reading
	// from stdoutIn and stderrIn.
	// wg ensures that we finish
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		copyAndCapture(os.Stdout, stdoutIn)
		wg.Done()
		if running {
			stderrIn.Close()
		}
	}()
	if !running {
		copyAndCapture(os.Stderr, stderrIn)
	}
	wg.Wait()
	err = cmd.Wait()
	if err != nil && !running {
		fmt.Printf("command: %s %s\n", execPath, cmdStr)
		fmt.Printf("Error : %v\n", err)
		os.Exit(1)
	}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func copyAndCapture(w io.Writer, r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			d := buf[:n]
			out = append(out, d...)
			_, err := w.Write(d)
			if strings.TrimSpace(string(d)) == "SDFS Volume Service Started" || strings.HasPrefix(strings.TrimSpace(string(d)), "Still running according to PID file") {
				running = true
				return out, nil
			}
			if err != nil {
				return out, err
			}
		}
		if err != nil {
			// Read returns io.EOF at the end of file, which is not an error for us
			if err == io.EOF {
				err = nil
			}
			return out, err
		}
	}
}
