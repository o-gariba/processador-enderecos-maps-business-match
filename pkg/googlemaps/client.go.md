Este pacote isola a lógica de chamada da API do Google.

**Struct `Client`:**
* `apiKey`: string
* `httpClient`: *http.Client (com timeout configurado)
* `limiter`: *rate.Limiter (instanciado no `worker/main.go` e injetado aqui)

**Função `NewClient(apiKey, limiter)`:**
* Retorna um `*Client` com um `http.Client` padrão e o `limiter`.

**Método `FindPlace(ctx, address)`:**
1.  **Esperar pelo Rate Limiter:** Chamar `client.limiter.Wait(ctx)`. Isso bloqueará a goroutine até que o "token" de taxa esteja disponível.
2.  Construir a URL da API `Find Place`:
    * Endpoint: `https://maps.googleapis.com/maps/api/place/findplacefromtext/json`
    * Parâmetros: `input=address`, `inputtype=textquery`, `fields=name,place_id`, `key=client.apiKey`.
3.  Criar um `http.NewRequestWithContext(ctx, "GET", url, nil)`.
4.  Executar a requisição: `client.httpClient.Do(req)`.
5.  Tratar erros de HTTP.
6.  Fazer `json.Unmarshal` da resposta.
7.  Retornar a struct de resultado (ex: `FindPlaceResult`) ou um erro.

**Struct `FindPlaceResult`:**
* `Name`: string `json:"name"`
* `PlaceID`: string `json:"place_id"`
* `Status`: string `json:"status"` // para checar "OK" ou "ZERO_RESULTS"
