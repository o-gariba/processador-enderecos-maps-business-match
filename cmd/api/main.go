package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/streadway/amqp"
)

var (
	db          *sqlx.DB
	minioClient *minio.Client
	rabbitCh    *amqp.Channel
	apiAuthKey  string
)

type Job struct {
	ID           string         `db:"id"`
	Status       string         `db:"status"`
	ResultPath   sql.NullString `db:"result_path"`
	ErrorMessage sql.NullString `db:"error_message"`
	CreatedAt    time.Time      `db:"created_at"`
	UpdatedAt    time.Time      `db:"updated_at"`
}

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
	apiAuthKey = os.Getenv("API_AUTH_KEY")

	// PostgreSQL
	db, err = sqlx.Connect("postgres", dbDSN)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	// RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	rabbitCh, err = conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer rabbitCh.Close()

	_, err = rabbitCh.QueueDeclare(
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
	minioClient, err = minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKeyID, minioSecretAccessKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalf("Failed to connect to MinIO: %v", err)
	}

	// Gin
	router := gin.Default()
	v1 := router.Group("/api/v1")
	v1.Use(authMiddleware())
	{
		jobs := v1.Group("/jobs")
		{
			jobs.POST("/upload", handleUploadCSV)
			jobs.GET("/:job_id", handleGetJobStatus)
		}
	}

	router.Run(":8080")
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is missing"})
			return
		}

		const bearerPrefix = "Bearer "
		if len(authHeader) < len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			return
		}

		token := authHeader[len(bearerPrefix):]
		if token != apiAuthKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			return
		}

		c.Next()
	}
}

func handleUploadCSV(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File is required"})
		return
	}

	if file.Header.Get("Content-Type") != "text/csv" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File must be a CSV"})
		return
	}

	if file.Size > 10*1024*1024 { // 10MB
		c.JSON(http.StatusBadRequest, gin.H{"error": "File size exceeds 10MB"})
		return
	}

	jobID := uuid.New()
	objectName := "uploads/" + jobID.String() + ".csv"

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()

	_, err = minioClient.PutObject(c.Request.Context(), "uploads", objectName, src, file.Size, minio.PutObjectOptions{ContentType: "text/csv"})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload file"})
		return
	}

	_, err = db.ExecContext(c.Request.Context(), "INSERT INTO jobs (id, status, created_at, updated_at) VALUES ($1, $2, $3, $4)", jobID, "PENDING", time.Now(), time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	jobMsg := map[string]string{
		"job_id":      jobID.String(),
		"caminho_csv": objectName,
	}
	msgBody, _ := json.Marshal(jobMsg)

	err = rabbitCh.Publish(
		"",           // exchange
		"jobs.queue", // routing key
		false,        // mandatory
		false,        // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        msgBody,
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to publish job"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID, "status": "PENDING"})
}

func handleGetJobStatus(c *gin.Context) {
	jobID := c.Param("job_id")

	var job Job
	err := db.GetContext(c.Request.Context(), &job, "SELECT * FROM jobs WHERE id = $1", jobID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get job status"})
		return
	}

	response := gin.H{
		"job_id": job.ID,
		"status": job.Status,
	}

	if job.Status == "COMPLETED" {
		if job.ResultPath.Valid {
			presignedURL, err := minioClient.PresignedGetObject(c.Request.Context(), "results", job.ResultPath.String, time.Second*60*60, nil) // 1 hour expiry
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate download URL"})
				return
			}
			response["download_url"] = presignedURL.String()
		}
	} else if job.Status == "FAILED" {
		if job.ErrorMessage.Valid {
			response["error"] = job.ErrorMessage.String
		}
	}

	c.JSON(http.StatusOK, response)
}
