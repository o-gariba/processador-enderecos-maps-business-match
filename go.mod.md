O nome do módulo deve ser: `github.com/o-gariba/processador-maps`

Este módulo Go precisará das seguintes dependências principais:

- **github.com/gin-gonic/gin**: Para o servidor da API (alternativa: `net/http` puro).
- **github.com/google/uuid**: Para gerar `job_id` únicos.
- **github.com/jmoiron/sqlx**: Para facilitar a interação com o PostgreSQL.
- **github.com/lib/pq**: O driver do PostgreSQL.
- **github.com/rabbitmq/amqp091-go**: O cliente oficial para RabbitMQ.
- **github.com/minio/minio-go/v7**: O cliente Go para MinIO (S3).
- **golang.org/x/time/rate**: Para implementar o Rate Limiter do Google Maps.
- **github.com/joho/godotenv**: (Opcional, para carregar .env em dev local)
