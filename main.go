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

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// === CONFIGURATION ===
type Config struct {
	YouTubeAPIKey        string `yaml:"YOUTUBE_API_KEY"`
	YouTubeVideoID       string `yaml:"YOUTUBE_VIDEO_ID"`
	ScrapeIntervalSecond int    `yaml:"SCRAPE_INTERVAL_SECONDS"`
}

var (
	apiKey         string
	videoID        string
	scrapeInterval = 30 * time.Second
)

func loadConfig() {
	// Valeurs par défaut depuis l'env
	cfg := Config{
		YouTubeAPIKey:        os.Getenv("YOUTUBE_API_KEY"),
		YouTubeVideoID:       os.Getenv("YOUTUBE_VIDEO_ID"),
		ScrapeIntervalSecond: 30,
	}
	// Flags CLI
	var (
		flagConfig   = pflag.String("config", "config.yaml", "Path to config file")
		flagAPIKey   = pflag.String("api-key", "", "YouTube API key (overrides config and env)")
		flagVideoID  = pflag.String("video-id", "", "YouTube video ID (overrides config and env)")
		flagInterval = pflag.Int("interval", 0, "Scrape interval in seconds (overrides config and env)")
	)
	pflag.Parse()
	// Surcharge via config.yaml si présent
	if _, err := os.Stat(*flagConfig); err == nil {
		data, err := ioutil.ReadFile(*flagConfig)
		if err != nil {
			log.Printf("[WARN] Could not read %s: %v", *flagConfig, err)
		} else {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				log.Printf("[WARN] Could not parse %s: %v", *flagConfig, err)
			} else {
				log.Printf("[INFO] Loaded configuration from %s", *flagConfig)
			}
		}
	}
	// Surcharge via CLI
	if *flagAPIKey != "" {
		cfg.YouTubeAPIKey = *flagAPIKey
	}
	if *flagVideoID != "" {
		cfg.YouTubeVideoID = *flagVideoID
	}
	if *flagInterval > 0 {
		cfg.ScrapeIntervalSecond = *flagInterval
	}
	apiKey = cfg.YouTubeAPIKey
	videoID = cfg.YouTubeVideoID
	scrapeInterval = time.Duration(cfg.ScrapeIntervalSecond) * time.Second
}

// === METRICS ===
var (
	liveViewers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "youtube_live_viewers",
		Help: "Number of concurrent viewers",
	})
	liveLikes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "youtube_live_likes",
		Help: "Number of likes on the livestream",
	})
	liveStatus = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "youtube_live_status",
		Help: "Live status: 1=live, 0=offline",
	})
)

// === STRUCTS ===
type YouTubeResponse struct {
	Items []struct {
		LiveStreamingDetails struct {
			ConcurrentViewers string `json:"concurrentViewers"`
			ActualStartTime   string `json:"actualStartTime"`
		} `json:"liveStreamingDetails"`
		Statistics struct {
			LikeCount string `json:"likeCount"`
		} `json:"statistics"`
	} `json:"items"`
}

// === FETCH METRICS ===
func fetchMetrics() {
	url := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/videos?part=liveStreamingDetails,statistics&id=%s&key=%s",
		videoID, apiKey,
	)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch YouTube API: %v", err)
		liveStatus.Set(0)
		return
	}
	defer resp.Body.Close()

	var ytResp YouTubeResponse
	if err := json.NewDecoder(resp.Body).Decode(&ytResp); err != nil {
		log.Printf("[ERROR] Failed to decode YouTube API response: %v", err)
		liveStatus.Set(0)
		return
	}

	if len(ytResp.Items) == 0 {
		log.Printf("[WARN] No live video found or invalid YOUTUBE_VIDEO_ID: %s", videoID)
		liveStatus.Set(0)
		return
	}

	item := ytResp.Items[0]

	// Viewers
	viewers := 0
	if item.LiveStreamingDetails.ConcurrentViewers != "" {
		v, err := strconv.Atoi(item.LiveStreamingDetails.ConcurrentViewers)
		if err != nil {
			log.Printf("[WARN] Failed to parse concurrentViewers: %v", err)
		} else {
			viewers = v
		}
	}
	liveViewers.Set(float64(viewers))

	// Likes
	likes := 0
	if item.Statistics.LikeCount != "" {
		l, err := strconv.Atoi(item.Statistics.LikeCount)
		if err != nil {
			log.Printf("[WARN] Failed to parse likeCount: %v", err)
		} else {
			likes = l
		}
	}
	liveLikes.Set(float64(likes))

	// Status
	if item.LiveStreamingDetails.ActualStartTime != "" {
		liveStatus.Set(1)
	} else {
		liveStatus.Set(0)
	}

	status := 0
	if item.LiveStreamingDetails.ActualStartTime != "" {
		status = 1
	}
	log.Printf("[INFO] Metrics updated: Viewers=%d, Likes=%d, Status=%d", viewers, likes, status)
}

func main() {
	loadConfig()
	if apiKey == "" || videoID == "" {
		log.Printf("[WARN] YOUTUBE_API_KEY and/or YOUTUBE_VIDEO_ID are not set (via CLI, config file or environment). Exporter will start, but metrics will be empty until values are provided.")
	} else {
		log.Printf("[INFO] Using YOUTUBE_API_KEY and YOUTUBE_VIDEO_ID from CLI, config file or environment.")
	}

	// Register metrics
	prometheus.MustRegister(liveViewers, liveLikes, liveStatus)
	log.Println("[INFO] Prometheus metrics registered.")

	// Start HTTP server
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		log.Println("[INFO] Fetching metrics (on /metrics request)...")
		fetchMetrics()
		promhttp.Handler().ServeHTTP(w, r)
	})
	log.Println("[INFO] Exporter running on :1907/metrics")
	if err := http.ListenAndServe(":1907", nil); err != nil {
		log.Fatalf("[FATAL] HTTP server failed: %v", err)
	}
}
