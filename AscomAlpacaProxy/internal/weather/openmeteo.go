package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync"
	"time"
)

// WeatherData holds the current weather metrics.
type WeatherData struct {
	Temperature   float64   `json:"temperature"`
	Humidity      float64   `json:"humidity"`
	DewPoint      float64   `json:"dew_point"`
	Pressure      float64   `json:"pressure"`
	WindSpeed     float64   `json:"wind_speed"`
	WindDir       float64   `json:"wind_direction"`
	WindGust      float64   `json:"wind_gusts"`
	CloudCover    float64   `json:"cloud_cover"`
	Precipitation float64   `json:"precipitation"`
	Timestamp     time.Time `json:"timestamp"`
}

type WeatherService struct {
	currentData *WeatherData
	mu          sync.RWMutex
	client      *http.Client
}

var (
	GlobalService *WeatherService
	serviceOnce   sync.Once
)

// GetService returns the singleton WeatherService instance.
func GetService() *WeatherService {
	serviceOnce.Do(func() {
		GlobalService = &WeatherService{
			client: &http.Client{Timeout: 15 * time.Second},
		}
	})
	return GlobalService
}

// GetData returns the latest cached weather data.
func (s *WeatherService) GetData() *WeatherData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentData
}

// Start spawns the background polling routine.
func (s *WeatherService) Start() {
	go s.run()
}

func (s *WeatherService) run() {
	logger.Info("WeatherService: Background poller started.")
	for {
		conf := config.Get()
		if !conf.EnableWeatherService {
			time.Sleep(1 * time.Minute)
			continue
		}

		if err := s.fetch(); err != nil {
			logger.Warn("WeatherService: Failed to fetch data: %v", err)
		}

		interval := conf.WeatherInterval
		if interval < 1 {
			interval = 5
		}
		time.Sleep(time.Duration(interval) * time.Minute)
	}
}

func (s *WeatherService) fetch() error {
	conf := config.Get()
	if conf.WeatherLatitude == 0 && conf.WeatherLongitude == 0 {
		return fmt.Errorf("coordinates not set")
	}

	modelSpec := conf.WeatherModel
	if modelSpec == "" || modelSpec == "best_match" {
		modelSpec = "best_match"
	}

	// URL for Open-Meteo Current Weather API
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.6f&longitude=%.6f&current=temperature_2m,relative_humidity_2m,dew_point_2m,surface_pressure,wind_speed_10m,wind_direction_10m,wind_gusts_10m,cloud_cover,precipitation&models=%s&timezone=auto",
		conf.WeatherLatitude, conf.WeatherLongitude, modelSpec)

	resp, err := s.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Current struct {
			Temperature   float64 `json:"temperature_2m"`
			Humidity      float64 `json:"relative_humidity_2m"`
			DewPoint      float64 `json:"dew_point_2m"`
			Pressure      float64 `json:"surface_pressure"`
			WindSpeed     float64 `json:"wind_speed_10m"`
			WindDir       float64 `json:"wind_direction_10m"`
			WindGust      float64 `json:"wind_gusts_10m"`
			CloudCover    float64 `json:"cloud_cover"`
			Precipitation float64 `json:"precipitation"`
		} `json:"current"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return err
	}

	s.mu.Lock()
	s.currentData = &WeatherData{
		Temperature:   apiResp.Current.Temperature,
		Humidity:      apiResp.Current.Humidity,
		DewPoint:      apiResp.Current.DewPoint,
		Pressure:      apiResp.Current.Pressure,
		WindSpeed:     apiResp.Current.WindSpeed,
		WindDir:       apiResp.Current.WindDir,
		WindGust:      apiResp.Current.WindGust,
		CloudCover:    apiResp.Current.CloudCover,
		Precipitation: apiResp.Current.Precipitation,
		Timestamp:     time.Now(),
	}
	s.mu.Unlock()

	logger.Debug("WeatherService: Successfully updated weather data using model '%s'.", modelSpec)
	return nil
}
