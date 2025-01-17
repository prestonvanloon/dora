package main

import (
	"context"
	"flag"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"

	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/handlers"
	"github.com/ethpandaops/dora/services"
	"github.com/ethpandaops/dora/static"
	"github.com/ethpandaops/dora/types"
	"github.com/ethpandaops/dora/utils"
)

func main() {
	configPath := flag.String("config", "", "Path to the config file, if empty string defaults will be used")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &types.Config{}
	err := utils.ReadConfig(cfg, *configPath)
	if err != nil {
		logrus.Fatalf("error reading config file: %v", err)
	}
	utils.Config = cfg
	logWriter, logger := utils.InitLogger()
	defer logWriter.Dispose()

	logger.WithFields(logrus.Fields{
		"config":  *configPath,
		"version": utils.BuildVersion,
		"release": utils.BuildRelease,
	}).Printf("starting")

	db.MustInitDB()
	err = db.ApplyEmbeddedDbSchema(-2)
	if err != nil {
		logger.Fatalf("error initializing db schema: %v", err)
	}

	err = services.StartChainService(ctx, logger)
	if err != nil {
		logger.Fatalf("error starting beacon service: %v", err)
	}

	err = services.StartTxSignaturesService()
	if err != nil {
		logger.Fatalf("error starting tx signature service: %v", err)
	}

	if cfg.RateLimit.Enabled {
		err = services.StartCallRateLimiter(cfg.RateLimit.ProxyCount, cfg.RateLimit.Rate, cfg.RateLimit.Burst)
		if err != nil {
			logger.Fatalf("error starting call rate limiter: %v", err)
		}
	}

	if cfg.Frontend.Enabled {
		err = services.StartFrontendCache()
		if err != nil {
			logger.Fatalf("error starting frontend cache service: %v", err)
		}

		startFrontend(logger)
	}

	utils.WaitForCtrlC()
	logger.Println("exiting...")
	db.MustCloseDB()
}

func startFrontend(logger logrus.FieldLogger) {
	router := mux.NewRouter()

	router.HandleFunc("/", handlers.Index).Methods("GET")
	router.HandleFunc("/index", handlers.Index).Methods("GET")
	router.HandleFunc("/index/data", handlers.IndexData).Methods("GET")
	router.HandleFunc("/clients/consensus", handlers.ClientsCL).Methods("GET")
	router.HandleFunc("/clients/execution", handlers.ClientsEl).Methods("GET")
	router.HandleFunc("/forks", handlers.Forks).Methods("GET")
	router.HandleFunc("/epochs", handlers.Epochs).Methods("GET")
	router.HandleFunc("/epoch/{epoch}", handlers.Epoch).Methods("GET")
	router.HandleFunc("/slots", handlers.Slots).Methods("GET")
	router.HandleFunc("/slots/filtered", handlers.SlotsFiltered).Methods("GET")
	router.HandleFunc("/slot/{slotOrHash}", handlers.Slot).Methods("GET")
	router.HandleFunc("/slot/{root}/blob/{commitment}", handlers.SlotBlob).Methods("GET")
	router.HandleFunc("/mev/blocks", handlers.MevBlocks).Methods("GET")

	router.HandleFunc("/search", handlers.Search).Methods("GET")
	router.HandleFunc("/search/{type}", handlers.SearchAhead).Methods("GET")
	router.HandleFunc("/validators", handlers.Validators).Methods("GET")
	router.HandleFunc("/validators/activity", handlers.ValidatorsActivity).Methods("GET")
	router.HandleFunc("/validators/deposits", handlers.Deposits).Methods("GET")
	router.HandleFunc("/validators/initiated_deposits", handlers.InitiatedDeposits).Methods("GET")
	router.HandleFunc("/validators/included_deposits", handlers.IncludedDeposits).Methods("GET")
	router.HandleFunc("/validators/voluntary_exits", handlers.VoluntaryExits).Methods("GET")
	router.HandleFunc("/validators/slashings", handlers.Slashings).Methods("GET")
	router.HandleFunc("/validator/{idxOrPubKey}", handlers.Validator).Methods("GET")
	router.HandleFunc("/validator/{index}/slots", handlers.ValidatorSlots).Methods("GET")

	router.HandleFunc("/identicon", handlers.Identicon).Methods("GET")

	if utils.Config.Frontend.Pprof {
		// add pprof handler
		router.PathPrefix("/debug/pprof/").Handler(http.DefaultServeMux)
	}

	if utils.Config.Frontend.Debug {
		// serve files from local directory when debugging, instead of from go embed file
		templatesHandler := http.FileServer(http.Dir("templates"))
		router.PathPrefix("/templates").Handler(http.StripPrefix("/templates/", templatesHandler))

		cssHandler := http.FileServer(http.Dir("static/css"))
		router.PathPrefix("/css").Handler(http.StripPrefix("/css/", cssHandler))

		jsHandler := http.FileServer(http.Dir("static/js"))
		router.PathPrefix("/js").Handler(http.StripPrefix("/js/", jsHandler))
	}

	fileSys := http.FS(static.Files)
	router.PathPrefix("/").Handler(handlers.CustomFileServer(http.FileServer(fileSys), fileSys, handlers.NotFound))

	n := negroni.New()
	n.Use(negroni.NewRecovery())
	//n.Use(gzip.Gzip(gzip.DefaultCompression))
	n.UseHandler(router)

	if utils.Config.Frontend.HttpWriteTimeout == 0 {
		utils.Config.Frontend.HttpWriteTimeout = time.Second * 15
	}
	if utils.Config.Frontend.HttpReadTimeout == 0 {
		utils.Config.Frontend.HttpReadTimeout = time.Second * 15
	}
	if utils.Config.Frontend.HttpIdleTimeout == 0 {
		utils.Config.Frontend.HttpIdleTimeout = time.Second * 60
	}
	srv := &http.Server{
		Addr:         utils.Config.Server.Host + ":" + utils.Config.Server.Port,
		WriteTimeout: utils.Config.Frontend.HttpWriteTimeout,
		ReadTimeout:  utils.Config.Frontend.HttpReadTimeout,
		IdleTimeout:  utils.Config.Frontend.HttpIdleTimeout,
		Handler:      n,
	}

	logger.Printf("http server listening on %v", srv.Addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			logger.WithError(err).Fatal("Error serving frontend")
		}
	}()
}
