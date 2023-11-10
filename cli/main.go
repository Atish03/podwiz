package main

import (
	"fmt"
	"github.com/Atish03/podwiz"
	"encoding/json"
	"github.com/olekukonko/tablewriter"
	"os"
	"flag"
)

type Creds struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Port uint16 `json:"ports"`
}

type ScheduleInfo struct {
	StartTime string `json:startTime`
	EndTime string `json:endTime`
	Name string `json:name`
	PodName string `json:podName`
}

func main() {
	conn := podwiz.Connect()

	var out []byte

	var name string
	var machineName string
	var path string
	var imgName string
	var timeToKill int
	var scheduleName string

	flag.StringVar(&name, "n", "", "-n <username>")
	flag.StringVar(&machineName, "m", "", "-m <machine name>")
	flag.StringVar(&path, "p", "", "-p <path to yaml file>")
	flag.StringVar(&imgName, "i", "", "-i <docker image name>")
	flag.IntVar(&timeToKill, "t", -1, "-t <time to kill>")
	flag.StringVar(&scheduleName, "sn", "", "-sn <name of schedule>")

	flag.Parse()
	arg := flag.Args()

	if len(arg) == 0 {
		out = []byte("hello")
	} else {
		switch arg[0] {
		case "start":
			allOp := true
			if name == "" {
				fmt.Println("provide a name for user, -n")
				allOp = false
			}
			if machineName == "" {
				fmt.Println("provide a name for machine, -m")
				allOp = false
			}
			if path == "" {
				fmt.Println("provide path of the folder containing Dockerfile and pod.yaml, -p")
				allOp = false
			}
			if imgName == "" {
				fmt.Println("provide docker image name, -i")
				allOp = false
			}
			if scheduleName == "" {
				fmt.Println("provide a schedule name for the scheduler, -sn")
				allOp = false
			}
			if timeToKill == -1 {
				fmt.Println("provide the time to kill, -t")
				allOp = false
			}
			if allOp {
				out = conn.Start(name, machineName, path, imgName, timeToKill, scheduleName)
				creds := Creds{}
				err := json.Unmarshal(out, &creds)
				if err != nil {
					fmt.Println("Server didnt send correct data!")
				}
				fmt.Printf("Username: %s\nPassword: %s\nPort: %d\n", creds.Username, creds.Password, creds.Port)
			} else {
				return
			}
		case "list":
			out = conn.List(scheduleName)
			schedules := []ScheduleInfo{}
			err := json.Unmarshal(out, &schedules)
			if err != nil {
				fmt.Println("Server didnt send correct data!")
			}
			toShow := [][]string{}
			for i := 0; i < len(schedules); i++ {
				toShow = append(toShow, []string{
					schedules[i].Name,
					schedules[i].PodName,
					schedules[i].StartTime,
					schedules[i].EndTime,
				})
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Name", "Pod name", "Start", "Finish"})

			for _, v := range toShow {
				table.Append(v)
			}
			table.Render()
		default:
			out = []byte("hello")
		}
	}
}