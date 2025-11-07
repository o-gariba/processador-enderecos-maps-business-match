# Projeto: Processador de Endereços (Maps Business Match)

## 🎯 Objetivo

Este projeto é um serviço de backend assíncrono projetado para processar grandes arquivos CSV de endereços. Para cada endereço, ele consulta a API do Google Maps para encontrar o "Business" (Google Meu Negócio) associado e retorna os dados (inicialmente, apenas `name` e `place_id`).

## 🏛️ Arquitetura

O sistema é desacoplado usando uma fila de mensagens para alta performance e resiliência.

1.  **API (`api-service`):** Um servidor Go que recebe um upload de CSV.
    * Valida o arquivo.
    * Salva o CSV bruto no MinIO (S3).
    * Cria um registro de "job" no PostgreSQL (status: `PENDING`).
    * Publica uma mensagem no RabbitMQ com o `job_id` e o caminho do arquivo.
    * Responde imediatamente ao usuário com o `job_id`.
2.  **Worker (`worker-service`):** Um serviço Go que consome da fila.
    * Recebe a mensagem do job.
    * Atualiza o status do job para `PROCESSING`.
    * Faz streaming do CSV a partir do MinIO (linha por linha).
    * Para cada linha, usa um pool de goroutines para processar em paralelo.
    * Cada goroutine chama a API do Google Maps (`Find Place` com `fields=name,place_id`).
    * Implementa um **Rate Limiter** global para não exceder o QPS do Google.
    * Salva os resultados (em formato JSONL) em um novo arquivo no MinIO.
    * Ao final, atualiza o status do job para `COMPLETED` no DB.

## 🛠️ Stack de Tecnologia

* **Linguagem:** Go (Golang) 1.21+
* **Banco de Dados:** PostgreSQL
* **Fila de Mensagens:** RabbitMQ
* **Storage de Objetos:** MinIO (API compatível com S3)
* **Orquestração:** Docker & Docker Compose

---

## 🚀 Como Executar

Siga os passos abaixo para configurar e executar o ambiente de desenvolvimento local.

### 1. Pré-requisitos

- [Docker](https://docs.docker.com/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

### 2. Configuração

1.  **Clone o repositório:**
    ```bash
    git clone <URL_DO_REPOSITORIO>
    cd processador-de-enderecos
    ```

2.  **Crie o arquivo de ambiente:**
    Copie o arquivo de exemplo `.env.example` para um novo arquivo chamado `.env`.
    ```bash
    cp .env.example .env
    ```

3.  **Preencha as variáveis no `.env`:**
    Abra o arquivo `.env` e preencha todas as variáveis. Elas são essenciais para a comunicação entre os serviços.
    - `POSTGRES_USER`, `POSTGRES_PASSWORD`: Credenciais para o banco de dados.
    - `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD`: Credenciais para o MinIO.
    - `API_AUTH_KEY`: Uma chave secreta para autenticar as requisições na API.
    - `GOOGLE_MAPS_API_KEY`: Sua chave de API do Google Maps.

### 3. Executando a Aplicação

Com o Docker em execução, suba todos os serviços com o Docker Compose:
```bash
docker-compose up --build
```
O comando `--build` garante que as imagens Docker serão reconstruídas caso haja alguma alteração no código.

---

## ✅ Verificando a Instalação

Após executar o `docker-compose up`, você pode verificar se tudo está funcionando corretamente.

### 1. Verifique os Contêineres

Liste os contêineres em execução para garantir que todos os 5 serviços estão ativos e saudáveis:
```bash
docker-compose ps
```
Você deve ver `api`, `worker`, `db`, `queue`, e `minio` com o status `Up` ou `running`.

### 2. Acesse as Interfaces Web

- **RabbitMQ:** Abra `http://localhost:15672` no seu navegador. Use `guest`/`guest` para fazer login.
- **MinIO:** Abra `http://localhost:9001` no seu navegador. Use as credenciais `MINIO_ROOT_USER` e `MINIO_ROOT_PASSWORD` que você definiu no `.env`.

### 3. Teste a API

Você pode usar uma ferramenta como `curl` ou Postman para testar o endpoint de upload.

1.  **Crie um arquivo de exemplo `enderecos.csv`:**
    ```csv
    Rua Funchal, 418, São Paulo, SP
    Avenida Brigadeiro Faria Lima, 3477, São Paulo, SP
    ```

2.  **Envie o arquivo para a API:**
    Substitua `sua_chave_secreta_para_api` pela chave que você definiu em `API_AUTH_KEY`.
    ```bash
    curl -X POST http://localhost:8080/api/v1/jobs/upload \
      -H "Authorization: Bearer sua_chave_secreta_para_api" \
      -F "file=@/caminho/para/seu/enderecos.csv"
    ```

3.  **Resposta Esperada (Upload):**
    A API deve responder imediatamente com um `202 Accepted` e o ID do job:
    ```json
    {
      "job_id": "a1b2c3d4-e5f6-g7h8-i9j0-k1l2m3n4o5p6",
      "status": "PENDING"
    }
    ```

4.  **Consulte o Status do Job:**
    Use o `job_id` retornado para consultar o status.
    ```bash
    curl http://localhost:8080/api/v1/jobs/a1b2c3d4-e5f6-g7h8-i9j0-k1l2m3n4o5p6 \
      -H "Authorization: Bearer sua_chave_secreta_para_api"
    ```

5.  **Resposta Esperada (Status):**
    - **Durante o processamento:**
      ```json
      {
        "job_id": "a1b2c3d4-e5f6-g7h8-i9j0-k1l2m3n4o5p6",
        "status": "PROCESSING"
      }
      ```
    - **Após a conclusão:**
      ```json
      {
        "job_id": "a1b2c3d4-e5f6-g7h8-i9j0-k1l2m3n4o5p6",
        "status": "COMPLETED",
        "download_url": "http://localhost:9000/results/results/a1b2c3d4-e5f6-g7h8-i9j0-k1l2m3n4o5p6.jsonl?..."
      }
      ```
      Você pode usar a `download_url` para baixar o arquivo de resultados do MinIO.
