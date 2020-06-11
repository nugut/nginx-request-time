package main

import (  
    "fmt"
    "flag"
    "time"
    "os/exec"
    "log"
    "strings"
    "regexp"
    "strconv"
    "sort"
    "path/filepath"
    "os"
    "net/http"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

type metric struct {
	name string
	help string
}

// cmd options
var log_path string
var date_by_log bool
var help bool
var http_port int

var url_metrics map[string]metric
var prometheus_metrics map[string]prometheus.Gauge

func recordMetrics() {
    go func() {
    	
		clf_months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	    percentiles := []int{80, 90, 95, 98}
		moscowLocation, _ := time.LoadLocation("Europe/Moscow")
		
		now := time.Now()
		time_limit := now.Add(-(time.Minute * 5))
		
		var err error
		var cmd_output []byte
		if date_by_log {
			// readd full log if time interval not used
			cmd_output, err = exec.Command("/bin/cat", log_path).Output()	
		} else {
			cmd_output, err = exec.Command("/usr/bin/tail", "-n10000", log_path).Output()	
		}		
		if err != nil {
			fmt.Println("Can't open log file for tail")
			log.Fatal(err)
		}
		log_strings := strings.Split(string(cmd_output), "\n")
		
		lines_cnt := len(log_strings) - 1 // last string is empty
		
		//months_cnt := len(clf_months)
		re := regexp.MustCompile(`^[0-9\.]+\s\S+\s\[` +
			// day
			`(\d{2})` +
			`\/` +
			// month
			`([a-zA-Z]+)` +
			`\/` + 
			// year
			`(\d{4})` + 
			`:` +
			// hours
			`(\d{2})` +
			`:` +
			// minutes
			`(\d{2})` +
			`:` +
			// seconds
			`(\d{2})` +
			`\s[-+0-9]+\]\s` +
			// request
			`(".*?")` +
			`\s` +
			// code
			`(\d+)` +
			`\s` +
			// request time
			`([0-9.]+)`,
		)
		
		var request_times []float64 
		for i := lines_cnt - 1; i >= 0; i -= 1 {
			log_string := log_strings[i]
			values := re.FindStringSubmatch(log_string)
			if len(values) > 0 {
				monthNum := Find(clf_months, values[2]) // find month index in abbreviations
				if monthNum == -1 {
					continue // bad format?
				}
				monthNum += 1 // month from 1 to 12
				year, _ := strconv.Atoi(values[3])
				day, _ := strconv.Atoi(values[1])
				hour, _ := strconv.Atoi(values[4])
				min, _ := strconv.Atoi(values[5])
				sec, _ := strconv.Atoi(values[6])
				time_from_log := time.Date(year, time.Month(monthNum), day, hour, min, sec, 0, moscowLocation)
				//fmt.Printf("time from log: %v\n", time_from_log)
				
				if !date_by_log && time_from_log.Before(time_limit) {
					break
				}
				request_time, _ := strconv.ParseFloat(values[9], 64)
				request_times = append(request_times, request_time) 
			}			
		}
		
		sort.Sort(sort.Reverse(sort.Float64Slice(request_times)))
		
		for _, percentile := range percentiles {
			metric_name := strconv.Itoa(percentile) + "_percentile"
			prometheus_metrics[metric_name].Set(calculate_percentile(percentile, request_times))	
		}	
    }()
}

func init() {
	
	url_metrics = map[string]metric{
		"80_percentile": metric {
			"nginxrt_percentile_80",
			"80 percentile of all backend requests",
		},
		"90_percentile": metric {
			"nginxrt_percentile_90",
			"90 percentile of all backend requests",
		},
		"95_percentile": metric {
			"nginxrt_percentile_95",
			"95 percentile of all backend requests",	
		},
		"98_percentile": metric {
			"nginxrt_percentile_98",
			"98 percentile of all backend requests",
		},
	}

	prometheus_metrics = make(map[string]prometheus.Gauge)

	for metric_name, prometheus_metric := range url_metrics {
		prometheus_metrics[metric_name] = promauto.NewGauge(prometheus.GaugeOpts{
			Name: prometheus_metric.name,
			Help: prometheus_metric.help,		
		})
	}
}    

func calculate_percentile(percentile int, data []float64) float64 {
	if len(data) > 0 {
		return float64(data[uint64(uint32(len(data) / 100) * uint32(100 - percentile))])	
	}
	return 0
}

func Find(a []string, searched_string string) int {
    for i, n := range a {
        if n == searched_string {
            return i
        }
    }
    return -1
}

func main() {
	
	flag.StringVar(&log_path, "f", "", "")
	flag.BoolVar(&date_by_log, "l", false, "")
	flag.BoolVar(&help, "h", false, "help")
	flag.IntVar(&http_port, "p", 9900, "")
	flag.Usage = func() {
		fmt.Printf("Usage of %s:\n", filepath.Base(os.Args[0]))
		fmt.Println("\t-f log_file\n\t\tpath to nginx access log")
		fmt.Println("\t-l\n\t\tparse log file tail without time limit")
		fmt.Println("\t-h\n\t\tthis message")
	}
	flag.Parse()
	if help {
		flag.Usage()
		os.Exit(0)
	}
	if len(log_path) == 0 {
		flag.Usage()
		os.Exit(1)
	}
	
	recordMetrics()
	
	http.Handle("/metrics", promhttp.Handler())
	
    http.ListenAndServe(":" + strconv.Itoa(http_port), nil)
} 
