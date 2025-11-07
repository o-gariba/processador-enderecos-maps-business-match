package processor

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"log"
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
}

// NewJobProcessor creates a new JobProcessor.
func NewJobProcessor(db *sqlx.DB, storage *minio.Client, mapsClient *googlemaps.Client) *JobProcessor {
	return &JobProcessor{
		db:         db,
		storage:    storage,
		mapsClient: mapsClient,
	}
}

// ProcessJob processes a CSV file of addresses.
func (p *JobProcessor) ProcessJob(ctx context.Context, jobID, csvPath string) {
	// Update job status to PROCESSING
	_, err := p.db.ExecContext(ctx, "UPDATE jobs SET status = $1, updated_at = $2 WHERE id = $3", "PROCESSING", time.Now(), jobID)
	if err != nil {
		log.Printf("Failed to update job status to PROCESSING for job %s: %v", jobID, err)
		return
	}

	resultPath := "results/" + jobID + ".jsonl"

	// MinIO read stream
	object, err := p.storage.GetObject(ctx, "uploads", csvPath, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("Failed to get object %s from MinIO: %v", csvPath, err)
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
			log.Printf("Failed to upload result file for job %s: %v", jobID, err)
			p.updateJobStatusToFailed(ctx, jobID, err)
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
				log.Printf("Failed to write result for job %s: %v", jobID, err)
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
				log.Printf("Error reading CSV for job %s: %v", jobID, err)
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
		log.Printf("Failed to update job status to COMPLETED for job %s: %v", jobID, err)
	}
}

func (p *JobProcessor) worker(ctx context.Context, wg *sync.WaitGroup, tasks <-chan string, results chan<- map[string]interface{}) {
	defer wg.Done()
	for address := range tasks {
		result, err := p.mapsClient.FindPlace(ctx, address)
		if err != nil {
			results <- map[string]interface{}{
				"address": address,
				"error":   err.Error(),
			}
			continue
		}
		results <- map[string]interface{}{
			"address": address,
			"result":  result,
		}
	}
}

func (p *JobProcessor) updateJobStatusToFailed(ctx context.Context, jobID string, err error) {
	_, updateErr := p.db.ExecContext(ctx, "UPDATE jobs SET status = $1, error_message = $2, updated_at = $3 WHERE id = $4", "FAILED", err.Error(), time.Now(), jobID)
	if updateErr != nil {
		log.Printf("Failed to update job status to FAILED for job %s: %v", jobID, updateErr)
	}
}
