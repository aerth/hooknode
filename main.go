package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/getsentry/raven-go"
	"github.com/iotaledger/giota"
	"github.com/joho/godotenv"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
)

type indexRequest struct {
	Trytes []giota.Trytes `json:"trytes"`
}

func init() {
	// Load ENV variables
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Setup sentry
	raven.SetDSN(os.Getenv("SENTRY_DSN"))
}

func main() {
	raven.CapturePanic(func() {

		// Attach handlers
		http.HandleFunc("/", raven.RecoveryHandler(indexHandler))
		http.HandleFunc("/stats", raven.RecoveryHandler(statsHandler))
		http.HandleFunc("/pow", powHandler)

		// Fetch port from ENV
		port := os.Getenv("PORT")
		fmt.Printf("\nListening on http://localhost:%v\n", port)

		// Start listening
		log.Fatal(http.ListenAndServe(":"+port, nil))

	}, nil)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Print("\nProcessing trytes\n")
	// POST only
	if r.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Unmarshal JSON
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Invalid request method", http.StatusBadRequest)
		return
	}

	defer r.Body.Close()

	req := indexRequest{}
	err = json.Unmarshal(b, &req)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// Convert []Trytes to []Transaction
	txs := make([]giota.Transaction, len(req.Trytes))
	for i, t := range req.Trytes {
		tx, _ := giota.NewTransaction(t)
		txs[i] = *tx
	}

	// Get configuration.
	provider := os.Getenv("PROVIDER")
	minDepth, _ := strconv.ParseInt(os.Getenv("MIN_DEPTH"), 10, 64)
	minWeightMag, _ := strconv.ParseInt(os.Getenv("MIN_WEIGHT_MAGNITUDE"), 10, 64)

	// Async sendTrytes
	api := giota.NewAPI(provider, nil)
	powname, pow := giota.GetBestPoW()
	fmt.Printf("Starting proof of work: %q\n", powname)

	c := make(chan struct{})

	go func(ch chan struct{}) {
		e := giota.SendTrytes(api, minDepth, txs, minWeightMag, pow)
		if e != nil {
			log.Println("error sending trytes:", e)
			raven.CaptureError(e, nil)
		}
		ch <- struct{}{}
	}(c)

	select {
	case <-time.After(time.Second * 10):
		log.Println("Timeout reached: 10 sec")
	case <-c:
		log.Println("Sent tx!")
	}

	w.WriteHeader(http.StatusNoContent)
}

func powHandler(w http.ResponseWriter, r *http.Request) {
	pow, _ := giota.GetBestPoW()

	body, err := json.Marshal(map[string]string{"powAlgo": pow})

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	c, _ := cpu.Percent(0, false)
	l, _ := load.Avg()
	m, _ := mem.VirtualMemory()

	body := map[string]interface{}{
		"cpu": map[string]interface{}{
			"avgPercent": c[0],
		},
		"load":   l,
		"memory": m,
	}

	res, _ := json.Marshal(body)

	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}
