Este pacote contém a lógica principal de processamento do job.

**Struct `JobProcessor`:**
* `db`: Repositório do banco de dados.
* `storage`: Cliente MinIO.
* `mapsClient`: Cliente do Google Maps (que contém o rate limiter).

**Método `ProcessJob(ctx, jobID, csvPath)`:**
1.  Atualizar status do job no DB para `PROCESSING`.
2.  Definir caminho do resultado: `results/{jobID}.jsonl`.
3.  **Abrir Stream de Leitura (MinIO):** Obter o objeto CSV (`csvPath`) do MinIO.
4.  **Abrir Stream de Escrita (MinIO):** Criar um `io.Pipe`. O `io.PipeWriter` será usado para escrever os resultados JSONL, e o `io.PipeReader` será passado para o `minio.PutObject` para fazer upload em streaming.
5.  Iniciar o upload em streaming em uma goroutine (`go minio.PutObject(..., pipeReader)`).
6.  Criar um `csv.NewReader` a partir do stream de leitura.
7.  **Pool de Workers (Goroutines):**
    * Definir um número de workers (ex: 50).
    * Criar canais: `tasks` (para enviar linhas do CSV) e `results` (para receber os JSONs processados).
    * Iniciar os workers (goroutines) que leem do canal `tasks`.
8.  **Goroutine (Escritor de Resultados):**
    * Iniciar uma goroutine que lê do canal `results`.
    * Para cada resultado, serializa para JSON (como uma linha) e escreve no `pipeWriter` (que está fazendo upload para o MinIO).
    * Quando o canal `results` fechar, fechar o `pipeWriter`.
9.  **Goroutine (Leitor de CSV):**
    * Ler o CSV linha por linha (ignorando o cabeçalho).
    * Para cada linha (endereço), enviar para o canal `tasks`.
    * Quando o CSV terminar, fechar o canal `tasks`.
10. **Lógica do Worker (dentro do pool):**
    * Para cada `task` (endereço) recebida:
    * Chamar `mapsClient.FindPlace(ctx, endereco)`. (O rate limiter interno do cliente cuidará da espera).
    * Formatar o resultado (sucesso ou erro).
    * Enviar o struct de resultado para o canal `results`.
11. **Finalização:**
    * Quando tudo terminar (leitor de CSV fechou `tasks`, workers terminaram, escritor de resultados fechou `pipeWriter`), atualizar o status do job no DB para `COMPLETED`.
    * Se ocorrer um erro, atualizar para `FAILED` com a mensagem de erro.
