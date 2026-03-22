package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/yaman/aws-credential-manager/core-go/internal/awssso"
	"github.com/yaman/aws-credential-manager/core-go/internal/awssts"
	"github.com/yaman/aws-credential-manager/core-go/internal/credentialsfile"
	"github.com/yaman/aws-credential-manager/core-go/internal/generator"
	"github.com/yaman/aws-credential-manager/core-go/internal/ipc"
	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
	onepasswordmanager "github.com/yaman/aws-credential-manager/core-go/internal/onepassword"
	"github.com/yaman/aws-credential-manager/core-go/internal/scheduler"
	"github.com/yaman/aws-credential-manager/core-go/internal/service"
	"github.com/yaman/aws-credential-manager/core-go/internal/sessioncache"
	"github.com/yaman/aws-credential-manager/core-go/internal/settings"
)

const version = "0.1.2"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	mode := "serve"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "serve":
		if err := serve(); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown mode: %s", mode)
	}
}

func serve() error {
	store, err := metadata.NewStore()
	if err != nil {
		return err
	}
	settingsStore, err := settings.NewStore()
	if err != nil {
		return err
	}
	opManager := onepasswordmanager.NewManager(settingsStore)
	credentialsStore, err := credentialsfile.NewStore("")
	if err != nil {
		return err
	}
	ssoService := awssso.New(sessioncache.New())
	generatorService := generator.New(
		opManager,
		store,
		credentialsStore,
		awssts.New(),
		ssoService,
	)
	router := service.NewRouter(version, store, settingsStore, opManager, generatorService)
	backgroundCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scheduler.New(store, generatorService, 60*time.Second).Start(backgroundCtx)
	go preloadSSOSessions(backgroundCtx, store, opManager, ssoService)

	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	var writerMu sync.Mutex
	var wg sync.WaitGroup

	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(line) == 0 {
			continue
		}

		wg.Add(1)
		go func(line []byte) {
			defer wg.Done()

			var response ipc.Response
			var request ipc.Request
			if err := json.Unmarshal(line, &request); err != nil {
				response = ipc.Failure("", "invalid_request", err.Error())
			} else {
				response = router.Handle(request)
			}

			writerMu.Lock()
			defer writerMu.Unlock()
			if err := writeResponse(writer, response); err != nil {
				log.Printf("write response failed: %v", err)
			}
		}(line)
	}

	wg.Wait()
	return scanner.Err()
}

func preloadSSOSessions(ctx context.Context, store *metadata.Store, opManager *onepasswordmanager.Manager, ssoService *awssso.Service) {
	preloadCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	index, err := store.Load()
	if err != nil {
		log.Printf("preload SSO sessions: metadata load failed: %v", err)
		return
	}
	for _, summary := range index.Configs {
		if summary.AuthType != "sso" {
			continue
		}
		input, err := opManager.LoadConfigItem(preloadCtx, summary)
		if err != nil {
			log.Printf("preload SSO sessions: config %s load failed: %v", summary.ID, err)
			continue
		}
		ssoService.PrimeFromInput(input)
	}
}

func writeResponse(writer *bufio.Writer, response ipc.Response) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, string(data)); err != nil {
		return err
	}
	return writer.Flush()
}
