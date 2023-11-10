package main

import (
	"github.com/Atish03/podwiz/app/spawner"
	"net"
	"log"
	"os"
	"syscall"
	"os/signal"
	"fmt"
	"time"
	"google.golang.org/protobuf/proto"
	"github.com/Atish03/podwiz/reqProto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"encoding/json"
)

type Creator struct {
	client spawner.Client
	schedules []*spawner.Scheduler
}

type Creds struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Ports uint16 `json:"ports"`
}

type ScheduleInfo struct {
	StartTime string `json:startTime`
	EndTime string `json:endTime`
	Name string `json:name`
	PodName string `json:podName`
}

type ToSend struct {
	Command string `json:command`
	Data []byte `json:data`
}

func main() {
	client := spawner.GetClient()
	allSchedules := []*spawner.Scheduler{}

	creator := Creator {
		client,
		allSchedules,
	}
	
	os.Remove("/tmp/podwiz.sock")
	socket, err := net.Listen("unix", "/tmp/podwiz.sock")
	if err != nil {
        log.Fatal(err)
    }

	c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        os.Remove("/tmp/podwiz.sock")
        os.Exit(1)
    }()

	for {
        conn, err := socket.Accept()
        if err != nil {
            log.Fatal(err)
        }

        go func(conn net.Conn) {
            defer conn.Close()
            buf := make([]byte, 4096)

            _, err := conn.Read(buf)
            if err == nil {
				msg := reqProto.Block{}
				proto.Unmarshal(buf, protoreflect.Message.Interface(msg.ProtoReflect()))

				switch {
				case msg.Command == "start":
					creds := creator.start(msg.Start.Name, msg.Start.MachineName, msg.Start.Path, msg.Start.ImgName, int(msg.Start.Time), msg.Start.ScheduleName)
					_, err = conn.Write(creds)
					if err != nil {
						log.Fatal(err)
					}
				case msg.Command == "list":
					schedules := creator.list(msg.List.ScheduleName)
					_, err = conn.Write(schedules)
					if err != nil {
						log.Fatal(err)
					}
				default:
					_, err = conn.Write([]byte("Command is not listed!"))
					if err != nil {
						log.Fatal(err)
					}
				}
			}
        }(conn)
    }
}

func (creator *Creator) start(name string, machineName string, path string, imgName string, time int, scheduleName string) []byte {
	s := spawner.New(time, scheduleName)
	user := creator.client.CreateUser(name, machineName, path, imgName)
	go s.Start(&user)
	creator.schedules = append(creator.schedules, s)

	creds := Creds {
		user.Username,
		user.Password,
		user.Port,
	}
	jsonData, err := json.Marshal(creds)
	if err != nil {
		fmt.Printf("could not marshal json: %s\n", err)
		return []byte("Internal Server Error")
	}

	send := ToSend {
		"start",
		jsonData,
	}
	marshalled, err := json.Marshal(send)
	if err != nil {
		fmt.Printf("could not marshal json: %s\n", err)
		return []byte("Internal Server Error")
	}

	return marshalled
}

func (creator *Creator) list(scheduleName string) []byte {
	allSchedules := []ScheduleInfo{}
	tmpSchedules := []*spawner.Scheduler{}

	for i := 0; i < len(creator.schedules); i++ {
		if (spawner.Scheduler{}) != *creator.schedules[i] {
			allSchedules = append(allSchedules, ScheduleInfo{
				StartTime: time.Unix(creator.schedules[i].StartTime, 0).String(),
				EndTime: time.Unix(creator.schedules[i].EndTime, 0).String(),
				Name: creator.schedules[i].Name,
				PodName: creator.schedules[i].User.Shell.PodName,
			})
			tmpSchedules = append(tmpSchedules, creator.schedules[i])
		}
	}

	creator.schedules = tmpSchedules

	jsonData, err := json.Marshal(allSchedules)
	if err != nil {
		fmt.Printf("could not marshal json: %s\n", err)
		return []byte("Internal Server Error")
	}

	send := ToSend {
		"list",
		jsonData,
	}
	marshalled, err := json.Marshal(send)
	if err != nil {
		fmt.Printf("could not marshal json: %s\n", err)
		return []byte("Internal Server Error")
	}

	return marshalled
}