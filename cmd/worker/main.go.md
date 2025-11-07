O `main.go` do Worker deve fazer o seguinte:

1.  **Carregar Configuração:** Ler as variáveis de ambiente (DB_DSN, RABBITMQ_URL, MINIO_ENDPOINT, GOOGLE_MAPS_API_KEY).
2.  **Inicializar Serviços:**
    * Conectar ao PostgreSQL.
    * Conectar ao RabbitMQ.
    * Conectar ao MinIO.
3.  **Criar Instâncias:**
    * Instanciar o repositório do DB.
    * Instanciar o cliente MinIO.
    * Instanciar o cliente do Google Maps (do `pkg/googlemaps`), passando um **Rate Limiter** global (ex: `rate.NewLimiter(rate.Limit(50), 50)` para 50 QPS).
4.  **Instanciar o Processador:** Criar uma instância do `JobProcessor` (de `internal/processor`), injetando as dependências (DB, MinIO, MapsClient).
5.  **Configurar Consumidor RabbitMQ:**
    * Declarar a fila `jobs.queue`.
    * Iniciar o consumo de mensagens (`channel.Consume`).
6.  **Loop de Consumo:**
    * Rodar em um loop infinito (`select {}`) para manter o worker vivo.
    * Para cada mensagem recebida (`delivery`):
        * Deserializar o JSON (`{"job_id": ..., "caminho_csv": ...}`).
        * Chamar o `jobProcessor.ProcessJob(ctx, jobID, csvPath)`.
        * Se o processamento for bem-sucedido, enviar `delivery.Ack(false)`.
        * Se falhar, enviar `delivery.Nack(false, true)` para re-enfileirar (ou enviar para uma dead-letter queue).
