# Projeto: Processador de Endereços (Maps Business Match)

## 🎯 Objetivo

Este projeto é um serviço de backend assíncrono projetado para processar grandes arquivos CSV de endereços. O objetivo principal é encontrar um **estabelecimento comercial** (ex: uma loja, restaurante, escritório) associado a cada endereço, usando um método robusto para maximizar a precisão.

Para cada endereço, o sistema consulta a API do Google Maps em um processo de três etapas:
1.  **Geocodificação (Geocoding API):** O endereço é convertido em coordenadas geográficas (latitude e longitude).
2.  **Busca por Proximidade (Nearby Search API):** Usando as coordenadas, o sistema busca por estabelecimentos comerciais em um raio pequeno (ex: 25 metros).
3.  **Detalhes do Local (Place Details API):** Se um estabelecimento é encontrado na busca por proximidade, seu `place_id` é usado para obter informações detalhadas, como o nome oficial do negócio, endereço formatado, telefone e website.

Este método é mais resiliente a variações no formato do endereço e aumenta a chance de encontrar um negócio em vez de apenas o endereço da rua.

## 🏛️ Arquitetura

O sistema é desacoplado usando uma fila de mensagens para alta performance e resiliência.

1.  **API (`api-service`):** Um servidor Go que recebe um upload de CSV.
    *   Valida o arquivo, salva no MinIO, cria um job no PostgreSQL e publica uma mensagem no RabbitMQ.
2.  **Worker (`worker-service`):** Um serviço Go que consome da fila.
    *   Recebe a mensagem do job e atualiza seu status.
    *   Processa cada endereço do CSV em um pool de goroutines.
    *   Cada goroutine executa um processo robusto de 3 etapas:
        *   **1. Geocodificação:** Converte o endereço em coordenadas (latitude/longitude) usando a **Geocoding API**.
        *   **2. Busca por Proximidade:** Procura por estabelecimentos em um raio de 25 metros ao redor das coordenadas usando a **Nearby Search API**.
        *   **3. Análise e Detalhes:** Analisa os resultados da busca para encontrar o primeiro que seja um tipo de negócio (ex: `store`, `establishment`). Se um negócio é encontrado, seu `place_id` é usado para buscar os detalhes finais com a **Place Details API**.
    *   Implementa um **Rate Limiter** global para não exceder o QPS do Google.
    *   Salva os resultados (em formato JSONL) em um novo arquivo no MinIO.
    *   Ao final, atualiza o status do job para `COMPLETED` no DB.

## 🏗️ Design Arquitetural

Cada componente da arquitetura tem uma responsabilidade clara e bem definida, seguindo o princípio da responsabilidade única.

*   **API (Ponto de Entrada):**
    *   **Responsabilidade:** Atuar como o ponto de entrada síncrono e rápido do sistema. Sua única função é validar a requisição, autenticar o usuário, aceitar o trabalho e enfileirá-lo para processamento assíncrono.
    *   **Justificativa:** Ao delegar a tarefa lenta (processamento de CSV) para a fila, a API pode responder ao cliente em milissegundos, garantindo uma excelente experiência do usuário e evitando timeouts.

*   **PostgreSQL (Fonte da Verdade):**
    *   **Responsabilidade:** Servir como o cérebro e a memória do sistema. Ele armazena o estado de cada job (`PENDING`, `PROCESSING`, `COMPLETED`, `FAILED`) e o caminho para o arquivo de resultado.
    *   **Justificativa:** Usar um banco de dados transacional garante a consistência e a durabilidade do estado dos jobs.

*   **RabbitMQ (O Desacoplador):**
    *   **Responsabilidade:** Atuar como um buffer de mensagens entre a API e os Workers. Ele absorve picos de requisições e garante que cada job será entregue a um Worker para processamento.
    *   **Justificativa:** A fila de mensagens é o que torna a arquitetura elástica e resiliente. Ela permite que a API e os Workers operem e escalem em ritmos diferentes.

*   **Worker (O Executor):**
    *   **Responsabilidade:** Executar a lógica de negócio principal, que é pesada e demorada. Ele consome jobs da fila, comunica-se com as APIs externas do Google, processa os dados e salva os resultados.
    *   **Justificativa:** Isolar o trabalho pesado em um componente separado permite que ele seja otimizado e escalado de forma independente, sem impactar a capacidade da API de receber novas requisições.

*   **MinIO (Armazenamento de Objetos):**
    *   **Responsabilidade:** Lidar com o armazenamento de arquivos grandes e não estruturados (os CSVs de entrada e os JSONLs de saída).
    *   **Justificativa:** Armazenar arquivos em um object storage como o MinIO (ou S3) é muito mais eficiente e escalável do que armazená-los em um banco de dados relacional ou em um sistema de arquivos local.

## ☁️ Cloud-Friendly por Design

Esta arquitetura foi projetada seguindo princípios modernos que a tornam ideal para implantação em ambientes de nuvem (AWS, GCP, Azure) e orquestradores de contêineres (Kubernetes, Docker Swarm).

*   **Escalabilidade Horizontal:** A separação entre a API e os Workers permite escalar cada um de forma independente. Se a fila de jobs crescer, basta adicionar mais réplicas do contêiner `worker-service` para aumentar o poder de processamento, sem afetar a performance da API.

*   **Resiliência e Tolerância a Falhas:** Se um Worker falhar no meio de um processamento, a mensagem na fila não é confirmada (`ack`) e o RabbitMQ a entregará para outro Worker disponível. Isso garante que nenhum job seja perdido. As `healthchecks` no Docker Compose também ajudam o sistema a se recuperar de falhas durante a inicialização.

*   **Observabilidade:** A implementação de logs estruturados (JSON) é uma prática recomendada para a nuvem. Esses logs podem ser facilmente coletados, indexados e pesquisados por qualquer plataforma de observabilidade (ex: Datadog, Splunk, AWS CloudWatch), permitindo um monitoramento e depuração eficientes.

*   **Serviços "Stateless":** A API e os Workers são "stateless" (sem estado). Todo o estado da aplicação é externalizado para serviços de backend (PostgreSQL, RabbitMQ, MinIO). Isso significa que qualquer contêiner da API ou do Worker pode ser parado, destruído ou substituído a qualquer momento sem perda de dados, o que é fundamental para a elasticidade e manutenção em ambientes de nuvem.

*   **Configuração Centralizada:** Toda a configuração é injetada por meio de variáveis de ambiente, seguindo o princípio da [App de 12 Fatores](https://12factor.net/config). Isso permite que a mesma imagem Docker seja promovida entre diferentes ambientes (desenvolvimento, homologação, produção) sem nenhuma alteração no código.

## 🛠️ Stack de Tecnologia

*   **Linguagem:** Go (Golang) 1.24+
*   **Banco de Dados:** PostgreSQL
*   **Fila de Mensagens:** RabbitMQ
*   **Storage de Objetos:** MinIO (API compatível com S3)
*   **Orquestração:** Docker & Docker Compose

---

## 🚀 Como Executar

Siga os passos abaixo para configurar e executar o ambiente de desenvolvimento local.

### 1. Pré-requisitos

-   [Docker](https://docs.docker.com/get-docker/)
-   [Docker Compose](https://docs.docker.com/compose/install/)

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
    Abra o arquivo `.env` e preencha todas as variáveis.
    -   `POSTGRES_USER`, `POSTGRES_PASSWORD`
    -   `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD`
    -   `API_AUTH_KEY`
    -   `GOOGLE_MAPS_API_KEY`

    **Importante sobre `GOOGLE_MAPS_API_KEY`:**
    Certifique-se de que sua chave de API tenha as seguintes APIs habilitadas no Google Cloud Console:
    *   **Geocoding API**
    *   **Places API** (que inclui Nearby Search e Place Details)
    
    Além disso, o projeto do Google Cloud associado à chave deve ter o **faturamento habilitado**.

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
```bash
docker-compose ps
```

### 2. Acesse as Interfaces Web

-   **RabbitMQ:** `http://localhost:15672` (guest/guest)
-   **MinIO:** `http://localhost:9001` (use as credenciais do `.env`)

### 3. Teste a API

1.  **Crie um arquivo de exemplo `enderecos.csv`:**
    ```csv
    Rua Coronel Luiz Venancio Martins, 577, Serra Azul, SP
    Avenida Brigadeiro Faria Lima, 3477, São Paulo, SP
    Rua Sem Negocio, 123, Cidade Ficticia, ZZ
    ```

2.  **Envie o arquivo para a API:**
    Substitua `sua_chave_secreta_para_api` pela chave que você definiu em `API_AUTH_KEY`.
    ```bash
    curl -X POST http://localhost:8080/api/v1/jobs/upload \
      -H "Authorization: Bearer sua_chave_secreta_para_api" \
      -F "file=@/caminho/para/seu/enderecos.csv"
    ```

3.  **Consulte o Status do Job:**
    Use o `job_id` retornado para consultar o status.
    ```bash
    curl http://localhost:8080/api/v1/jobs/<job_id> \
      -H "Authorization: Bearer sua_chave_secreta_para_api"
    ```

4.  **Formato do Arquivo de Resultados (`.jsonl`):**
    O arquivo de resultados será um JSONL (JSON Lines), onde cada linha é um objeto JSON.
    
    *   **Exemplo de Sucesso (Estabelecimento Encontrado):**
        ```json
        {"address":"Rua Coronel Luiz Venancio Martins, 577, Serra Azul, SP","place_id":"ChIJ4TW-jrTTuZQRpouXgmjigr0","details":{"result":{"name":"Supermercado Serra Azul","formatted_address":"R. Cel. Luiz Venâncio Martins, 577 - Centro, Serra Azul - SP, 14230-000, Brazil", ...},"status":"OK"}}
        ```
    
    *   **Exemplo de Falha (Nenhum Estabelecimento Encontrado):**
        Neste caso, o `place_id` retornado será o do endereço geocodificado, se disponível.
        ```json
        {"address":"Rua Sem Negocio, 123, Cidade Ficticia, ZZ","place_id":"ChIJrQiO-82pzpQRVd28J5-b9y4","details":null,"status":"NO_ESTABLISHMENT_FOUND"}
        ```
