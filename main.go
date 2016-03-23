package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

type serverResponse struct {
	TimeDelay int32
}

var serverStatus chan int
var responseTimes chan time.Duration
var tickets chan bool
var monitorServer bool

func main() {
	serverStatus = make(chan int, 3)
	responseTimes = make(chan time.Duration, 3)
	tickets = make(chan bool)

	monitorServer = true
	go monitorServerStatus()
	go generateTicket()

	fmt.Printf("Listening on localhost:9002, will request data from JSON server.\n")
	http.HandleFunc("/", returnJSON)         // set router
	err := http.ListenAndServe(":9002", nil) // set listen port
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
	fmt.Println("Closing monitor")
	monitorServer = false
}

func returnJSON(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received request...")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	status := <-serverStatus
	if status == 1 {
		fmt.Println("Circuit Breaker Detected Failure, No return made.")
		http.Error(w, "Circuit Breaker Detected Failure", http.StatusInternalServerError)
		return
	} else if status == 2 {
		ticket := <-tickets
		if ticket {
			fmt.Println("Circuit Breaker limiting traffic, No return made.")
			http.Error(w, "Circuit Breaker Limiting Traffic", http.StatusInternalServerError)
			return
		}
		fmt.Println("Circuit Breaker limiting traffic, return will be attempted.")
	}
	fmt.Println("Making Request to JSON Server.")
	response, elapsed, err := getJSONAndRequestTime()
	responseTimes <- elapsed

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Println("Returning: " + string(body[:]))

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func getJSONAndRequestTime() (response *http.Response, elapsed time.Duration, err error) {
	start := time.Now()
	response, err = http.Get("http://localhost:9001/")
	elapsed = time.Since(start)
	return
}

func monitorServerStatus() {
	var status = 0
	fmt.Println("Beginning JSON server monitoring.")
	for monitorServer {
		fmt.Println("Attempting to connect to JSON server...")
		_, responseTime, err := getJSONAndRequestTime()

		if err != nil {
			fmt.Println("Server did not response, circuit breaker will stop connections.")
			status = 1
		} else if responseTime <= time.Second*time.Duration(5) {
			fmt.Println("Server responded in less than 5 seconds, circuit breaker will allow all traffic.")
			status = 0
		} else {
			fmt.Println("Server responded in over 5 seconds, circuit breaker will restrict traffic.")
			status = 2
		}

		start := time.Now()
		for time.Since(start) < time.Second*time.Duration(5) {
			var channelOpen = false
			select {
			case serverStatus <- status:
				channelOpen = true
				fmt.Println("Server status set in channel: ", status)
			default:
				fmt.Println("Server status already in channel.")
			}

			select {
			case responseTime = <-responseTimes:
				channelOpen = true
				if status == 0 && responseTime > time.Second*time.Duration(5) {
					status = 2
				}
			default:
				fmt.Println("No response time available in channel.")
			}
			if !channelOpen {
				fmt.Println("No channels open, sleeping for 1 second.")
				time.Sleep(time.Second * time.Duration(1))
			}

		}
	}
}

func generateTicket() {
	var ticket = true
	for monitorServer {
		tickets <- ticket
		ticket = !ticket
	}
}
