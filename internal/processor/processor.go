package processor

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/minio/minio-go/v7"

	"processador-de-enderecos/pkg/googlemaps"
)

// JobProcessor holds the dependencies for processing a job.
type JobProcessor struct {
	db         *sqlx.DB
	storage    *minio.Client
	mapsClient *googlemaps.Client
	logger     *slog.Logger
}

// NewJobProcessor creates a new JobProcessor.
func NewJobProcessor(db *sqlx.DB, storage *minio.Client, mapsClient *googlemaps.Client, logger *slog.Logger) *JobProcessor {
	return &JobProcessor{
		db:         db,
		storage:    storage,
		mapsClient: mapsClient,
		logger:     logger,
	}
}

// ProcessJob processes a CSV file of addresses.
func (p *JobProcessor) ProcessJob(ctx context.Context, jobID, csvPath string) {
	jobLogger := p.logger.With("job_id", jobID)

	// Update job status to PROCESSING
	_, err := p.db.ExecContext(ctx, "UPDATE jobs SET status = $1, updated_at = $2 WHERE id = $3", "PROCESSING", time.Now(), jobID)
	if err != nil {
		jobLogger.Error("Failed to update job status to PROCESSING", "error", err)
		return
	}

	resultPath := "results/" + jobID + ".jsonl"

	// MinIO read stream
	object, err := p.storage.GetObject(ctx, "uploads", csvPath, minio.GetObjectOptions{})
	if err != nil {
		jobLogger.Error("Failed to get object from MinIO", "bucket", "uploads", "path", csvPath, "error", err)
		p.updateJobStatusToFailed(ctx, jobID, err)
		return
	}
	defer object.Close()

	// MinIO write stream
	pipeReader, pipeWriter := io.Pipe()

	var wgUpload sync.WaitGroup
	wgUpload.Add(1)
	go func() {
		defer wgUpload.Done()
		_, err := p.storage.PutObject(ctx, "results", resultPath, pipeReader, -1, minio.PutObjectOptions{ContentType: "application/jsonl"})
		if err != nil {
			jobLogger.Error("Failed to upload result file to MinIO", "bucket", "results", "path", resultPath, "error", err)
			// We can't reliably update the job status here as the main function might have already exited
		}
	}()

	csvReader := csv.NewReader(object)

	// Worker pool
	numWorkers := 50
	tasks := make(chan string)
	results := make(chan map[string]interface{})

	var wgWorkers sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wgWorkers.Add(1)
		go p.worker(ctx, &wgWorkers, tasks, results)
	}

	// Result writer goroutine
	var wgResultWriter sync.WaitGroup
	wgResultWriter.Add(1)
	go func() {
		defer wgResultWriter.Done()
		jsonlWriter := json.NewEncoder(pipeWriter)
		for result := range results {
			if err := jsonlWriter.Encode(result); err != nil {
				jobLogger.Warn("Failed to write result to JSONL stream", "error", err)
			}
		}
		pipeWriter.Close()
	}()

	// CSV reader goroutine
	go func() {
		// Skip header
		_, _ = csvReader.Read()
		for {
			record, err := csvReader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				jobLogger.Error("Error reading CSV file", "error", err)
				break
			}
			tasks <- record[0]
		}
		close(tasks)
	}()

	wgWorkers.Wait()
	close(results)
	wgResultWriter.Wait()
	wgUpload.Wait()

	// Update job status to COMPLETED
	_, err = p.db.ExecContext(ctx, "UPDATE jobs SET status = $1, result_path = $2, updated_at = $3 WHERE id = $4", "COMPLETED", resultPath, time.Now(), jobID)
	if err != nil {
		jobLogger.Error("Failed to update job status to COMPLETED", "error", err)
	} else {
		jobLogger.Info("Job completed successfully")
	}
}

func (p *JobProcessor) worker(ctx context.Context, wg *sync.WaitGroup, tasks <-chan string, results chan<- map[string]interface{}) {
	defer wg.Done()
	for address := range tasks {
		// Step 1: Geocode the address to get coordinates and a fallback place_id
		geocodeResponse, err := p.mapsClient.Geocode(ctx, address)
		if err != nil {
			p.logger.Warn("Failed to geocode address", "address", address, "error", err)
			results <- map[string]interface{}{"address": address, "error": err.Error()}
			continue
		}

		if geocodeResponse.Status == "ZERO_RESULTS" || len(geocodeResponse.Results) == 0 {
			results <- map[string]interface{}{"address": address, "status": "NO_RESULTS_FOUND"}
			continue
		}

		// Use the first result for coordinates and as a fallback
		firstResult := geocodeResponse.Results[0]
		lat := firstResult.Geometry.Location.Lat
		lng := firstResult.Geometry.Location.Lng
		fallbackPlaceID := firstResult.PlaceID

		// Step 2: Perform a Nearby Search for establishments
		nearbyResponse, err := p.mapsClient.NearbySearch(ctx, lat, lng, 25) // 25-meter radius
		if err != nil {
			p.logger.Warn("Nearby Search failed", "address", address, "lat", lat, "lng", lng, "error", err)
			results <- map[string]interface{}{"address": address, "place_id": fallbackPlaceID, "status": "NEARBY_SEARCH_FAILED"}
			continue
		}

		var establishmentPlaceID string
		if nearbyResponse.Status == "OK" && len(nearbyResponse.Results) > 0 {
			// Find the first result that is explicitly a business
			for _, place := range nearbyResponse.Results {
				isBusiness := false
				for _, t := range place.Types {
					// Check for common business-related types.
					if t == "establishment" || t == "point_of_interest" || t == "store" || t == "supermarket" || t == "restaurant" {
						isBusiness = true
						break
					}
				}
				if isBusiness {
					establishmentPlaceID = place.PlaceID
					break // Found a good candidate, stop searching
				}
			}
		}

		// Step 3: Get details if an establishment was found
		if establishmentPlaceID != "" {
			detailsResult, err := p.mapsClient.GetPlaceDetails(ctx, establishmentPlaceID)
			if err != nil {
				p.logger.Warn("Failed to get place details for establishment", "address", address, "place_id", establishmentPlaceID, "error", err)
				results <- map[string]interface{}{"address": address, "place_id": establishmentPlaceID, "status": "GET_DETAILS_FAILED"}
				continue
			}
			results <- map[string]interface{}{
				"address":  address,
				"place_id": establishmentPlaceID,
				"details":  detailsResult,
			}
		} else {
			// If no establishment was found nearby, output the fallback
			results <- map[string]interface{}{
				"address":  address,
				"place_id": fallbackPlaceID,
				"details":  nil,
				"status":   "NO_ESTABLISHMENT_FOUND",
			}
		}
	}
}

func (p *JobProcessor) updateJobStatusToFailed(ctx context.Context, jobID string, err error) {
	jobLogger := p.logger.With("job_id", jobID)
	_, updateErr := p.db.ExecContext(ctx, "UPDATE jobs SET status = $1, error_message = $2, updated_at = $3 WHERE id = $4", "FAILED", err.Error(), time.Now(), jobID)
	if updateErr != nil {
		jobLogger.Error("Failed to update job status to FAILED", "original_error", err, "update_error", updateErr)
	}
}
