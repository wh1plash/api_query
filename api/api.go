package api

import (
	"api_query/internal"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	rdb      *redis.Client
	db       *sql.DB
	cacheTTL = time.Minute * 5 // TTL для кеша Redis
)

func init() {
	cfg := internal.MustLoad()
	// Инициализация Redis
	rdb = redis.NewClient(&redis.Options{
		Addr:         cfg.Redis,
		DB:           0,
		PoolSize:     20,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 20 * time.Second,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Инициализация Postgres

	pgConnStr := cfg.Postgres
	var err error
	db, err = sql.Open("postgres", pgConnStr)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	if err = db.Ping(); err != nil {
		log.Fatalf("Failed to ping PostgreSQL: %v", err)
	}
}

type APIServer struct {
	ListenAddr  string
	ReqTimeout  time.Duration
	IdleTimeout time.Duration
	//quitch      chan struct{}
}

func NewAPIServer(listenAddr string, reqTimeout, idleTimeout time.Duration) *APIServer {
	return &APIServer{
		ListenAddr:  listenAddr,
		ReqTimeout:  reqTimeout,
		IdleTimeout: idleTimeout,
	}
}

type apiError struct {
	Error string `json:"error"`
}

//	type requestQuery struct {
//		Query string `json:"query"`
//	}
type apiFunc func(http.ResponseWriter, *http.Request) error

func (s *APIServer) Start() {
	router := mux.NewRouter()
	router.HandleFunc("/query", makeHTTPHandleFunc(s.handleQueryToCSv))

	router.HandleFunc("/", makeHTTPHandleFunc(s.indexHandler))
	log.Printf("Listening on %s", s.ListenAddr)
	log.Fatal(http.ListenAndServe(s.ListenAddr, router))

}

func (s *APIServer) handleQueryToCSv(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "method not allowed"})
	}
	//
	//req := requestQuery{}
	//if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
	//	http.Error(w, err.Error(), http.StatusBadRequest)
	//}
	//query := strings.TrimSpace(req.Query)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return err
	}

	query := r.FormValue("query")
	query = strings.TrimSpace(query)
	if query == "" {
		http.Error(w, "Query is empty", http.StatusBadRequest)
		return writeJSON(w, http.StatusBadRequest, apiError{Error: "Query is empty"})
	}

	// Используем хэш запроса как ключ для кеша
	cacheKey := fmt.Sprintf("query_cache:%x", query)

	start := time.Now()
	// Проверяем наличие данных в кеше
	if existsInCache(cacheKey) {
		log.Println("Cache hit. Exporting cached result to CSV.")
		cachedData, err := getDataFromCache(cacheKey)
		if err != nil {
			return writeJSON(w, http.StatusInternalServerError, apiError{Error: "Failed to retrieve cached data"})
		}
		if err := internal.SaveToCSV(strings.NewReader(cachedData)); err != nil {
			return writeJSON(w, http.StatusInternalServerError, apiError{Error: "Failed to save cached data to CSV"})
		}
		// Возврат кешированных данных как CSV
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=\"result.csv\"")
		w.WriteHeader(http.StatusOK)
		//w.Write([]byte(cachedData))
		log.Println("Start streaming payload to client from cached data...")
		log.Println("bytes of payload: ", len(cachedData))
		io.Copy(w, bytes.NewReader([]byte(cachedData)))

		return nil
		//return writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Data exported to CSV. Time taken: %v", time.Since(start))})
	}

	log.Println("Cache miss. Executing SQL query.")

	// Выполняем SQL-запрос
	rows, err := db.Query(query)
	if err != nil {
		return writeJSON(w, http.StatusInternalServerError, apiError{Error: "Failed to execute query"})
	}
	defer func() { _ = rows.Close() }()

	log.Printf("SQL query from Postgres executed in %v", time.Since(start))

	// Преобразуем результат в CSV-строку
	var csvBuilder strings.Builder
	if err := internal.RowsToCSV(rows, &csvBuilder); err != nil {
		log.Printf("Failed to convert rows to CSV: %v", err)
	}

	// Сохраняем результат в кеш
	csvData := csvBuilder.String()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		if err := cachedData(cacheKey, csvData); err != nil {
			log.Printf("Failed to cache query result: %v", err)
		}
	}(ctx)
	wg.Wait() // Ожидание завершения всех горутин

	// Сохраняем результат в CSV-файл
	if err := internal.SaveToCSV(strings.NewReader(csvBuilder.String())); err != nil {
		log.Printf("Failed to save data to CSV: %v", err)

	}

	//return writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Data exported to CSV. Time taken: %v", time.Since(start))})
	//Возврат данных как CSV
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=\"result.csv\"")
	w.WriteHeader(http.StatusOK)
	log.Println("Start streaming payload to client from Postgres query data...")
	log.Println("bytes of payload: ", csvBuilder.Len())
	//w.Write([]byte("<div id='result'><h2>Result:</h2><p>" + csvBuilder.String() + "</p></div>"))
	io.Copy(w, bytes.NewReader([]byte(csvBuilder.String())))
	return nil
}

func (s *APIServer) indexHandler(w http.ResponseWriter, r *http.Request) error {
	tmpl, err := template.ParseFiles("index.templ")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		log.Printf("Template parse error: %v", err)
		return err
	}

	if err := tmpl.Execute(w, nil); err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		log.Printf("Template execute error: %v", err)
	}
	return nil
}

func existsInCache(cacheKey string) bool {
	_, err := rdb.Get(context.Background(), cacheKey+":meta").Result()
	return err == nil
}
func getDataFromCache(cacheKey string) (string, error) {
	// Получаем метаинформацию
	meta, err := rdb.Get(context.Background(), cacheKey+":meta").Result()
	if err != nil {
		return "", fmt.Errorf("failed to get cache meta: %v", err)
	}

	// Читаем количество частей
	var partCount int
	if _, err := fmt.Sscanf(meta, "parts:%d", &partCount); err != nil {
		return "", fmt.Errorf("invalid meta format: %v", err)
	}

	// Собираем все части
	var result strings.Builder
	for i := 0; i < partCount; i++ {
		partKey := fmt.Sprintf("%s:part:%d", cacheKey, i)
		part, err := rdb.Get(context.Background(), partKey).Result()
		if err != nil {
			return "", fmt.Errorf("failed to get cache part %d: %v", i, err)
		}
		result.WriteString(part)
	}
	return result.String(), nil
}

func cachedData(cacheKey, data string) error {
	const chunkSize = 10 * 1024 * 1024 // Размер части: 1 МБ

	// Разделяем данные на части
	parts := splitIntoChunks(data, chunkSize)

	// Сохраняем части в Redis
	for i, part := range parts {
		partKey := fmt.Sprintf("%s:part:%d", cacheKey, i)
		if err := rdb.Set(context.Background(), partKey, part, cacheTTL).Err(); err != nil {
			return fmt.Errorf("failed to cache part %d: %v", i, err)
		}
	}

	// Сохраняем метаинформацию
	meta := fmt.Sprintf("parts:%d", len(parts))
	if err := rdb.Set(context.Background(), cacheKey+":meta", meta, cacheTTL).Err(); err != nil {
		return fmt.Errorf("failed to cache meta: %v", err)
	}
	return nil
}

func splitIntoChunks(data string, chunkSize int) []string {
	var chunks []string
	for len(data) > chunkSize {
		chunks = append(chunks, data[:chunkSize])
		data = data[chunkSize:]
	}
	if len(data) > 0 {
		chunks = append(chunks, data)
	}
	return chunks
}

func makeHTTPHandleFunc(f apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		}
	}
}
func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}
