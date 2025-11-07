O `main.go` da API deve fazer o seguinte:

1.  **Carregar Configuração:** Ler as variáveis de ambiente (DB_DSN, RABBITMQ_URL, MINIO_ENDPOINT, etc).
2.  **Inicializar Serviços:**
    * Conectar ao PostgreSQL (`sqlx.Connect`).
    * Conectar ao RabbitMQ e declarar a fila (ex: `jobs.queue`).
    * Conectar ao MinIO e garantir que os buckets (`uploads`, `results`) existam.
3.  **Criar Instâncias:**
    * Instanciar o repositório do banco de dados.
    * Instanciar o publicador do RabbitMQ.
    * Instanciar o cliente MinIO.
4.  **Configurar Servidor HTTP (Gin):**
    * Criar um router `gin.Default()`.
    * Adicionar um middleware de autenticação (verificar `API_AUTH_KEY` no header `Authorization: Bearer <key>`).
5.  **Definir Rotas:**
    * `POST /api/v1/jobs/upload`: Mapear para o handler `handleUploadCSV`.
    * `GET /api/v1/jobs/:job_id`: Mapear para o handler `handleGetJobStatus`.
6.  **Iniciar Servidor:** Rodar na porta `8080`.

**Handler `handleUploadCSV`:**
* Recebe um `multipart/form-data` com o campo `file`.
* Valida o arquivo (ex: tamanho < 10MB, tipo `text/csv`).
* Gera um `job_id` (UUID).
* Define o nome do arquivo no MinIO (ex: `uploads/{job_id}.csv`).
* Faz upload do arquivo para o bucket `uploads` no MinIO.
* Cria um registro no DB (tabela `jobs`) com o `job_id` e status `PENDING`.
* Publica uma mensagem JSON no RabbitMQ (fila `jobs.queue`) contendo: `{"job_id": "...", "caminho_csv": "uploads/{job_id}.csv"}`.
* Retorna `202 Accepted` com o JSON: `{"job_id": "...", "status": "PENDING"}`.

**Handler `handleGetJobStatus`:**
* Pega o `job_id` da URL.
* Consulta o DB pelo `job_id`.
* Se "COMPLETED", retorna o status e uma URL pré-assinada para download do resultado no MinIO (bucket `results`).
* Se "PENDING" ou "PROCESSING", retorna o status.
* Se "FAILED", retorna o status e a mensagem de erro.
