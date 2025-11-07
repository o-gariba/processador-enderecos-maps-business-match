package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/streadway/amqp"
	"golang.org/x/time/rate"

	"processador-de-enderecos/internal/processor"
	"processador-de-enderecos/pkg/googlemaps"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}

	// Config
	dbDSN := os.Getenv("DB_DSN")
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	minioEndpoint := os.Getenv("MINIO_ENDPOINT")
	minioAccessKeyID := os.Getenv("MINIO_ACCESS_KEY_ID")
	minioSecretAccessKey := os.Getenv("MINIO_SECRET_ACCESS_KEY")
	googleMapsAPIKey := os.Getenv("GOOGLE_MAPS_API_KEY")

	// PostgreSQL
	db, err := sqlx.Connect("postgres", dbDSN)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	// RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"jobs.queue", // name
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// MinIO
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKeyID, minioSecretAccessKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalf("Failed to connect to MinIO: %v", err)
	}

	// Google Maps Client
	limiter := rate.NewLimiter(rate.Limit(50), 50)
	mapsClient := googlemaps.NewClient(googleMapsAPIKey, limiter)

	// Job Processor
	jobProcessor := processor.NewJobProcessor(db, minioClient, mapsClient)

	// RabbitMQ Consumer
	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	var forever chan struct{}

	go func() {
		for d := range msgs {
			var jobData map[string]string
			if err := json.Unmarshal(d.Body, &jobData); err != nil {
				log.Printf("Error decoding job data: %s", err)
				d.Nack(false, false) // To dead-letter queue
				continue
			}

			jobID := jobData["job_id"]
			csvPath := jobData["caminho_csv"]

			go jobProcessor.ProcessJob(context.Background(), jobID, csvPath)
			d.Ack(false)
		}
	}()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever
}
