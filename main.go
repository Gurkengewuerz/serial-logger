package main

import (
	"bytes"
	"fmt"
	"github.com/akamensky/argparse"
	"go.bug.st/serial"
	"golang.org/x/net/html/charset"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	TimeDiff   = int64(0)
	isPi       = false
	isSyncTime = false
	dataChan   = make(chan []byte)
	deleteChan = make(chan int)
)

func getNow() string {
	// 2006-01-02T15:04:05-0700
	return time.Now().Add(time.Duration(TimeDiff) * time.Second).Format(time.RFC3339)
}

func runForPort(i int, portName string, mode *serial.Mode) bool {

	port, err := serial.Open(portName, mode)
	if err != nil {
		//log.Printf("Failed to open port %v: %v", portName, err)
		return false
	}

	defer port.Close()

	f, err := os.OpenFile(fmt.Sprintf("differentmind_%v.txt", i), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("error opening file: %v", err)
		return false
	}
	defer f.Close()

	_, _ = f.WriteString("---------------- new logging started ----------------\r\n")

	if isPi {
		exec.Command("gpio", "write", string(rune(8+i)), "1")
	}

	readError := false
	delFile := false
	stringBuffer := bytes.NewBufferString("")
	stringBuffer.Grow(1024)
	buf := make([]byte, 512)

	go func() {
		for {
			toDel := <-deleteChan
			//log.Print("received message", toDel)
			if toDel == i {
				delFile = true
				return
			}
		}
	}()

	// Must be in another routine because there is no non blocking read
	go func() {
		for {
			n, err := port.Read(buf)
			if err != nil || n == 0 || delFile {
				readError = true
				return
			}
			stringBuffer.WriteString(string(buf[:n]))

			if strings.Contains(stringBuffer.String(), "\n") {
				s, err := stringBuffer.ReadString('\n')
				if err != nil {
					continue
				}
				cleanText := strings.Replace(s, "\r", "", -1)
				cleanText = strings.Replace(cleanText, "\n", "", -1)

				if len(cleanText) <= 0 || cleanText == "" {
					continue
				}

				if cleanText[0] == 0x00 {
					cleanText = cleanText[1:]
				}

				withDate := fmt.Sprintf("[%v] %v", getNow(), cleanText)
				reader, _ := charset.NewReaderLabel("ascii", bytes.NewReader([]byte(withDate)))
				strBytes, _ := io.ReadAll(reader)

				_, _ = f.Write(strBytes)
				_, _ = f.WriteString("\n")
				_ = f.Sync()

				wsMsg := fmt.Sprintf("{%v}%v", i, withDate)
				wsReader, _ := charset.NewReaderLabel("ascii", bytes.NewReader([]byte(wsMsg)))
				wsStrBytes, _ := io.ReadAll(wsReader)

				select {
				case dataChan <- wsStrBytes:
					//fmt.Println("sent message")
				default:
					//fmt.Println("no message sent")
				}

				if isSyncTime {
					t, err := time.Parse("2006-01-02 15:04:05", strings.Replace(cleanText, "# ", "", -1))
					if err == nil {
						log.Println("---------------- syncing time ----------------")
						TimeDiff = t.Unix() - time.Now().Unix()
						log.Println("time difference is ", TimeDiff)
					}
				}
			}
		}
	}()

	for {
		if delFile {
			f.Close()
			_ = os.Remove(fmt.Sprintf("differentmind_%v.txt", i))
			return true
		}
		if readError {
			break
		}
	}

	_, _ = f.WriteString("---------------- logging ended ----------------\r\n\r\n\r\n")

	if isPi {
		exec.Command("gpio", "write", string(rune(8+i)), "0")
	}

	if readError {
		log.Printf("Failed to read port %v", portName)
		return false
	}
	return true
}

func whileRun(i int, port string, config *serial.Mode) {
	if isPi {
		exec.Command("gpio", "mode", string(rune(8+i)), "out")
	}

	for {
		runForPort(i, port, config)
		time.Sleep(5 * time.Second)
	}
}

func main() {
	parser := argparse.NewParser("seriallogger", "by Niklas Schütrumpf <niklas@mc8051.de>")
	ports := parser.List("p", "port", &argparse.Options{Required: true, Help: "COM ports which should be scanned"})
	pi := parser.Flag("g", "gpio", &argparse.Options{Required: false, Help: "If RPi GPIO are supported use this flag"})
	syncTime := parser.Flag("s", "sync-time", &argparse.Options{Required: false, Help: "Sync time using the output of a serial port"})

	err := parser.Parse(os.Args)
	if err != nil {
		log.Print(parser.Usage(err))
		os.Exit(0)
	}

	isPi = *pi
	isSyncTime = *syncTime

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Exit(1)
	}()

	l, err := net.Listen("tcp", "0.0.0.0:9669")
	if err != nil {
		log.Print(err)
		os.Exit(0)
	}
	log.Printf("listening on http://%v", l.Addr())

	cs := newChatServer(&dataChan, &deleteChan, len(*ports))
	s := &http.Server{
		Handler:      cs,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()

	mode := serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	for i, s := range *ports {
		if i >= 3 {
			continue
		}

		go whileRun(i, s, &mode)
	}

	for {
		time.Sleep(10 * time.Second)
	}
}
