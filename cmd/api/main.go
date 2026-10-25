// @title Empre Backend API
// @version 1.0
// @description Interactive API documentation for the Local Discovery App.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @BasePath /
// @query.collection.format multi

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer <your-token>" to authenticate.
package main

import (
	"context"
	"log"
	"time"

	"empre_backend/config"
	"empre_backend/internal/database"
	"empre_backend/internal/handlers"
	"empre_backend/internal/middleware"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/internal/services"
	"empre_backend/internal/websocket"

	_ "empre_backend/docs"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load Config
	cfg := config.LoadConfig()
	// Un secreto débil firma tokens que cualquiera podría falsificar: en producción no se arranca con él.
	secretWarn, secretErr := config.ValidateJWTSecret(cfg.JWTSecret, cfg.IsProduction())
	if secretErr != nil {
		log.Fatal("Configuración insegura: ", secretErr)
	}
	if secretWarn != "" {
		log.Println("Aviso de seguridad: ", secretWarn)
	}

	// Connect to Database
	database.ConnectDB(cfg)

	// Deja lista la tabla entity_photos para la migración al modelo Post
	// (ver el comentario de PrepareForPostMigration): sin esto, AutoMigrate
	// falla la primera vez que corre sobre una base de datos de antes de Post.
	if err := database.PrepareForPostMigration(database.DB); err != nil {
		log.Fatal("No se pudo preparar la base de datos para la migración de Post: ", err)
	}

	// Auto Migrate
	err := database.DB.AutoMigrate(
		&models.User{},
		&models.Entity{},
		&models.Message{},
		&models.Category{},
		&models.Subcategory{},
		&models.Media{},
		&models.Post{},
		&models.EntityPhoto{},
		&models.PasswordResetToken{},
		&models.RefreshToken{},
		&models.Review{},
		&models.Favorite{},
		&models.PushToken{},
		&models.BusinessHour{},
		&models.Subscription{},
		&models.SubscriptionPayment{},
		&models.Offer{},
		&models.OfferClaim{},
		&models.BusinessStatus{},
		&models.Signal{},
		&models.Promotion{},
		&models.CategoryRelation{},
		&models.BusinessCategory{},
		&models.Place{},
		&models.Zone{},
		&models.Event{},
		&models.ServiceItem{},
		&models.OfferItem{},
		&models.PromotionItem{},
		&models.AgendaSettings{},
		&models.Appointment{},
		&models.Party{},
		&models.Sale{},
		&models.SaleLine{},
		&models.Expense{},
		&models.DebtPayment{},
		&models.VoiceDraft{},
	)
	if err != nil {
		log.Fatal("Migration failed: ", err)
	}

	// Restricción anti-solapamiento de la agenda (AutoMigrate no crea exclusiones).
	if err := database.EnsureAgendaConstraints(database.DB); err != nil {
		log.Fatal("No se pudo crear la restricción de la agenda: ", err)
	}

	// Seed default categories (and their subcategories) on a fresh database
	database.SeedCategories(database.DB)
	database.SeedSubcategories(database.DB)

	// Migraciones de datos (idempotentes): extensiones de búsqueda tolerante,
	// jerarquía de categorías, business_categories, places y sinónimos.
	if err := database.EnableSearchExtensions(database.DB); err != nil {
		log.Fatal("No se pudieron habilitar las extensiones de búsqueda: ", err)
	}
	if err := database.MigrateHierarchy(database.DB); err != nil {
		log.Fatal("No se pudo migrar la jerarquía de categorías: ", err)
	}
	if err := database.MigratePlaces(database.DB); err != nil {
		log.Fatal("No se pudo migrar los places: ", err)
	}
	if err := services.SeedZones(database.DB); err != nil {
		log.Fatal("No se pudieron sembrar las zonas: ", err)
	}
	if err := services.MigrateEntityZones(database.DB); err != nil {
		log.Fatal("No se pudo asignar la zona a los negocios: ", err)
	}
	if err := database.SeedFamilies(database.DB); err != nil {
		log.Fatal("No se pudieron asignar las familias: ", err)
	}
	if err := database.SeedSynonyms(database.DB); err != nil {
		log.Fatal("No se pudieron sembrar los sinónimos: ", err)
	}
	if err := database.EnsureClaimIndexes(database.DB); err != nil {
		log.Fatal("No se pudo crear el índice de reclamos abiertos: ", err)
	}

	// Initialize Router
	r := gin.Default()

	// Enable CORS
	r.Use(middleware.CORSMiddleware())

	// Initialize Repositories
	userRepo := repository.NewUserRepository(database.DB)
	categoryRepo := repository.NewCategoryRepository(database.DB)
	entityRepo := repository.NewEntityRepository(database.DB)
	mediaRepo := repository.NewMediaRepository(database.DB)
	chatRepo := repository.NewChatRepository(database.DB)
	passwordResetRepo := repository.NewPasswordResetRepository(database.DB)
	refreshTokenRepo := repository.NewRefreshTokenRepository(database.DB)
	reviewRepo := repository.NewReviewRepository(database.DB)
	favoriteRepo := repository.NewFavoriteRepository(database.DB)
	pushTokenRepo := repository.NewPushTokenRepository(database.DB)
	subscriptionRepo := repository.NewSubscriptionRepository(database.DB)
	offerRepo := repository.NewOfferRepository(database.DB)
	claimRepo := repository.NewClaimRepository(database.DB)
	signalRepo := repository.NewSignalRepository(database.DB)
	promotionRepo := repository.NewPromotionRepository(database.DB)
	searchRepo := repository.NewSearchRepository(database.DB)

	// Initialize Services
	storageService := services.NewStorageService(cfg)
	mediaService := services.NewMediaService(mediaRepo, storageService, cfg.AppURL)

	var mailerService services.MailerService
	if cfg.SMTPHost != "" {
		mailerService = services.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPSender)
		log.Println("Email Service: SMTP initialized")
	} else {
		mailerService = services.NewConsoleMailer()
		log.Println("Email Service: Console fallback initialized")
	}

	authService := services.NewAuthService(userRepo, passwordResetRepo, refreshTokenRepo, mailerService, cfg)
	userService := services.NewUserService(userRepo, mediaService)
	entityService := services.NewEntityService(entityRepo, mediaService)
	zoneService := services.NewZoneService(database.DB)
	entityService.Zones = zoneService
	categoryService := services.NewCategoryService(categoryRepo)
	chatService := services.NewChatService(chatRepo)
	reviewService := services.NewReviewService(reviewRepo, claimRepo)
	favoriteService := services.NewFavoriteService(favoriteRepo)
	pushService := services.NewPushService(pushTokenRepo)
	subscriptionService := services.NewSubscriptionService(subscriptionRepo, chatService, pushService, entityService, cfg.WompiPublicKey, cfg.WompiIntegritySecret, cfg.WompiEventsSecret, cfg.WompiRedirectURL)
	if subscriptionService.Enabled() {
		log.Println("Pagos: Wompi habilitado")
	} else {
		log.Println("Pagos: deshabilitados (faltan WOMPI_PUBLIC_KEY / WOMPI_INTEGRITY_SECRET)")
	}
	agendaService := services.NewAgendaService(database.DB, pushService)
	aiService := services.NewAIService(cfg.AIAPIKey, cfg.AIBaseURL, cfg.AIModel)
	if aiService.Enabled() {
		log.Println("AI Assistant: enabled (model " + cfg.AIModel + ")")
	} else {
		log.Println("AI Assistant: disabled (no AI_API_KEY)")
	}

	offerService := services.NewOfferService(offerRepo, entityRepo)
	signalService := services.NewSignalService(signalRepo)
	claimService := services.NewClaimService(claimRepo, offerRepo, entityRepo, userRepo, signalService)
	rankingService := services.NewRankingService(entityService, reviewService, offerRepo, signalService)
	promotionService := services.NewPromotionService(promotionRepo, entityRepo)
	offerListService := services.NewOfferListService(offerRepo, promotionRepo)
	searchService := services.NewSearchService(searchRepo, offerRepo)

	// Initialize Handlers
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService, mediaService)
	mediaHandler := handlers.NewMediaHandler(mediaService)
	entityHandler := handlers.NewEntityHandler(entityService, mediaService, database.DB, reviewService, favoriteService, pushService, signalService)
	categoryHandler := handlers.NewCategoryHandler(categoryService)
	reviewHandler := handlers.NewReviewHandler(reviewService, userService, entityService, pushService)
	favoriteHandler := handlers.NewFavoriteHandler(favoriteService, entityService, reviewService, pushService, signalService)
	pushHandler := handlers.NewPushHandler(pushService)
	aiHandler := handlers.NewAIHandler(aiService, categoryService)
	paymentHandler := handlers.NewPaymentHandler(subscriptionService, entityService)
	offerHandler := handlers.NewOfferHandler(offerService)
	claimHandler := handlers.NewClaimHandler(claimService)
	discoveryHandler := handlers.NewDiscoveryHandler(rankingService)
	statusHandler := handlers.NewStatusHandler(database.DB)
	zoneHandler := handlers.NewZoneHandler(zoneService)
	cellHandler := handlers.NewCellHandler(services.NewCellService(database.DB))
	reviewSnippetHandler := handlers.NewReviewSnippetHandler(services.NewReviewSnippetService(database.DB))
	postFeedHandler := handlers.NewPostFeedHandler(services.NewPostFeedService(database.DB, mediaService))
	eventService := services.NewEventService(database.DB)
	eventHandler := handlers.NewEventHandler(eventService)
	catalogHandler := handlers.NewCatalogHandler(services.NewCatalogService(database.DB))
	// Retención: los eventos de uso de más de 180 días se borran al arrancar.
	if n, err := eventService.PurgeOlderThan(time.Now().AddDate(0, 0, -180)); err != nil {
		log.Println("No se pudieron purgar eventos viejos: ", err)
	} else if n > 0 {
		log.Printf("Eventos de uso purgados: %d", n)
	}
	promotionHandler := handlers.NewPromotionHandler(promotionService)
	offerListHandler := handlers.NewOfferListHandler(offerListService)
	offerMarketHandler := handlers.NewOfferMarketHandler(services.NewOfferMarketService(database.DB, reviewService))
	completenessHandler := handlers.NewCompletenessHandler(services.NewCompletenessService(database.DB))
	searchHandler := handlers.NewSearchHandler(searchService)
	agendaHandler := handlers.NewAgendaHandler(agendaService)
	ledgerService := services.NewLedgerService(database.DB)
	ledgerService.SetVoiceModel(aiService)
	ledgerHandler := handlers.NewLedgerHandler(ledgerService)
	inboxHandler := handlers.NewInboxHandler(services.NewInboxService(database.DB))

	wsHub := websocket.NewHub(database.DB, pushService, subscriptionService)
	go wsHub.Run()
	// Expira los reclamos vencidos y libera sus cupos (vive lo que el proceso).
	go claimService.RunExpirySweeper(time.Minute, nil)
	chatHandler := handlers.NewChatHandler(wsHub, chatService)

	// La agenda empuja cambios por WebSocket y corre su proceso de fondo
	// (expiración, recordatorios y cierre) cada 30 segundos.
	agendaService.SetRealtime(wsHub)
	agendaService.StartAgendaJobs(context.Background())

	// Routes
	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken)
			auth.POST("/password-reset/request", authHandler.RequestPasswordReset)
			auth.POST("/password-reset/reset", authHandler.ResetPassword)
		}

		entities := api.Group("/entities")
		{
			// Public viewing (Discovery)
			entities.GET("", entityHandler.FindAll)
			entities.GET("/:id", middleware.OptionalAuth(cfg), entityHandler.FindByID)
			entities.GET("/:id/reviews", reviewHandler.FindByEntity)
			entities.GET("/:id/posts", postFeedHandler.List)
			entities.GET("/:id/offer", offerHandler.Active)
			entities.GET("/:id/offers/active", offerHandler.ActiveList)
			entities.GET("/:id/activity", claimHandler.Activity)
			entities.GET("/:id/status", statusHandler.Get)
			entities.GET("/:id/promotions/active", promotionHandler.Active)
			entities.GET("/:id/catalog", catalogHandler.List)

			// Protected mutations
			entitiesProtected := entities.Use(middleware.AuthMiddleware(cfg))
			{
				entitiesProtected.POST("", entityHandler.Create)
				entitiesProtected.GET("/mine", entityHandler.FindAllByOwner)
				entitiesProtected.PUT("/:id", entityHandler.Update)
				// Catálogo de servicios (solo el dueño; la lectura es pública, arriba).
				entitiesProtected.POST("/:id/catalog", catalogHandler.Create)
				entitiesProtected.POST("/:id/catalog/confirm", catalogHandler.Confirm)
				entitiesProtected.PUT("/:id/catalog/:itemId", catalogHandler.Update)
				entitiesProtected.DELETE("/:id/catalog/:itemId", catalogHandler.Retire)
				entitiesProtected.DELETE("/:id", entityHandler.Delete)
				entitiesProtected.POST("/:id/images", entityHandler.UploadImage)
				entitiesProtected.DELETE("/:id/images/:photoId", entityHandler.DeleteImage)
				entitiesProtected.PATCH("/:id/images/:photoId", entityHandler.UpdateImageCaption)
				entitiesProtected.POST("/:id/favorite", favoriteHandler.Add)
				entitiesProtected.DELETE("/:id/favorite", favoriteHandler.Remove)
				entitiesProtected.GET("/:id/followers", favoriteHandler.FollowerCount)
				entitiesProtected.POST("/:id/reviews", reviewHandler.Upsert)
				entitiesProtected.DELETE("/:id/reviews", reviewHandler.Delete)
				entitiesProtected.GET("/:id/reviews/mine", reviewHandler.FindMine)
				entitiesProtected.GET("/:id/subscription", paymentHandler.GetSubscription)
				entitiesProtected.POST("/:id/subscription/checkout", paymentHandler.CreateCheckout)
				entitiesProtected.POST("/:id/offers", offerHandler.Publish)
				entitiesProtected.GET("/:id/offers", offerHandler.List)
				entitiesProtected.GET("/:id/offers/stats", offerHandler.Stats)
				entitiesProtected.GET("/:id/completeness", completenessHandler.Get)
				entitiesProtected.PATCH("/:id/status", statusHandler.Update)
				entitiesProtected.POST("/:id/promotions", promotionHandler.Publish)
				entitiesProtected.GET("/:id/promotions", promotionHandler.List)

				// Agenda de citas
				entitiesProtected.GET("/:id/agenda/summary", agendaHandler.Summary)
				entitiesProtected.GET("/:id/agenda/availability", agendaHandler.Availability)
				entitiesProtected.GET("/:id/agenda", agendaHandler.BusinessView)
				entitiesProtected.GET("/:id/agenda/settings", agendaHandler.GetSettings)
				entitiesProtected.PUT("/:id/agenda/settings", agendaHandler.UpdateSettings)
				entitiesProtected.GET("/:id/agenda/services", agendaHandler.ListServices)
				entitiesProtected.POST("/:id/agenda/services", agendaHandler.CreateService)
				entitiesProtected.PUT("/:id/agenda/services/:serviceId", agendaHandler.UpdateService)
				entitiesProtected.DELETE("/:id/agenda/services/:serviceId", agendaHandler.DeleteService)
				entitiesProtected.POST("/:id/agenda/blocks", agendaHandler.CreateBlock)
				entitiesProtected.DELETE("/:id/agenda/blocks/:blockId", agendaHandler.DeleteBlock)

				// Administración del negocio (solo el dueño)
				entitiesProtected.POST("/:id/ledger/sales", ledgerHandler.CreateSale)
				entitiesProtected.GET("/:id/ledger/sales", ledgerHandler.ListSales)
				entitiesProtected.POST("/:id/ledger/sales/:saleId/void", ledgerHandler.VoidSale)
				entitiesProtected.POST("/:id/ledger/expenses", ledgerHandler.CreateExpense)
				entitiesProtected.GET("/:id/ledger/expenses", ledgerHandler.ListExpenses)
				entitiesProtected.GET("/:id/ledger/expenses/usual", ledgerHandler.UsualExpenses)
				entitiesProtected.POST("/:id/ledger/expenses/:expenseId/void", ledgerHandler.VoidExpense)
				entitiesProtected.POST("/:id/ledger/parties", ledgerHandler.CreateParty)
				entitiesProtected.GET("/:id/ledger/parties", ledgerHandler.ListParties)
				entitiesProtected.GET("/:id/ledger/parties/:partyId/payments", ledgerHandler.ListPayments)
				entitiesProtected.POST("/:id/ledger/payments", ledgerHandler.CreatePayment)
				entitiesProtected.GET("/:id/ledger/items", ledgerHandler.ListItems)
				entitiesProtected.PUT("/:id/ledger/items/:itemId/cost", ledgerHandler.SetItemCost)
				entitiesProtected.GET("/:id/ledger/summary", ledgerHandler.Summary)
				entitiesProtected.GET("/:id/ledger/export", ledgerHandler.Export)
				entitiesProtected.POST("/:id/ledger/voice/parse", ledgerHandler.VoiceParse)
				entitiesProtected.POST("/:id/ledger/voice/:draftId/confirm", ledgerHandler.VoiceConfirm)
				entitiesProtected.POST("/:id/ledger/voice/:draftId/discard", ledgerHandler.VoiceDiscard)
			}
		}

		categories := api.Group("/categories")
		{
			// Public viewing. Mutations live under /admin (admin only).
			categories.GET("", categoryHandler.FindAll)
			categories.GET("/:id", categoryHandler.FindByID)
		}

		// Descubrimiento: ranking contextual + diversificación (público).
		api.GET("/discovery", discoveryHandler.Discover)
		api.GET("/discovery/sections", discoveryHandler.Sections)
		// Negocios agrupados en casillas para el mapa lejano (público).
		api.GET("/discovery/cells", cellHandler.List)
		// La frase de reseña de los destacados (público).
		api.GET("/discovery/review-snippets", reviewSnippetHandler.List)

		// Zonas con nombre de una ciudad (público).
		api.GET("/zones", zoneHandler.List)

		// Medición de uso anónima (público, con límite de peticiones).
		api.POST("/events", middleware.RateLimit(30, time.Minute), eventHandler.Ingest)

		// Pestaña Ofertas: relámpagos y promociones activos (público).
		api.GET("/offers", offerListHandler.List)
		// Vitrina de ofertas (3.38): pública; con sesión suma «Recomendado para ti».
		api.GET("/offers/market", middleware.OptionalAuth(cfg), middleware.RateLimit(60, time.Minute), offerMarketHandler.Market)
		api.GET("/offers/market/all", middleware.RateLimit(120, time.Minute), offerMarketHandler.MarketAll)

		// Búsqueda por niveles + chips dinámicos (público).
		api.GET("/search", searchHandler.Search)
		api.GET("/categories/chips", searchHandler.Chips)

		// Ofertas: acciones del negocio (quick actions, republish) y reclamo
		// del usuario. La publicación/listado vive bajo /entities/:id/offers.
		offers := api.Group("/offers")
		offers.Use(middleware.AuthMiddleware(cfg))
		{
			// «Este mes ahorraste» (3.48): solo con sesión.
			offers.GET("/savings", offerMarketHandler.MonthSavings)
			// «Completa tu plan» (3.49): después de reclamar.
			offers.GET("/:id/next", offerMarketHandler.Next)
			offers.PATCH("/:id", offerHandler.QuickAction)
			offers.POST("/:id/republish", offerHandler.Republish)
			offers.POST("/:id/claim", middleware.RateLimit(5, time.Minute), claimHandler.Claim)
		}

		// Promociones: edición/estado (la publicación/listado vive bajo
		// /entities/:id/promotions).
		promotions := api.Group("/promotions")
		promotions.Use(middleware.AuthMiddleware(cfg))
		{
			promotions.PATCH("/:id", promotionHandler.Update)
		}

		// Reclamos y canje (autenticados). El canje lleva un límite más alto
		// porque el negocio escanea varios códigos seguidos.
		claims := api.Group("/claims")
		claims.Use(middleware.AuthMiddleware(cfg))
		{
			claims.GET("/mine", claimHandler.Mine)
			claims.GET("/:id", claimHandler.Get)
			claims.DELETE("/:id", claimHandler.Cancel)
			claims.POST("/:id/redeem", middleware.RateLimit(20, time.Minute), claimHandler.Redeem)
			claims.POST("/:id/confirm", claimHandler.Confirm)
			claims.POST("/redeem-code", middleware.RateLimit(20, time.Minute), claimHandler.RedeemCode)
		}

		// WebSocket & Chat History
		chatGroup := api.Group("/chat")
		chatGroup.Use(middleware.AuthMiddleware(cfg))
		{
			chatGroup.GET("/ws", chatHandler.HandleWebSocket)
			chatGroup.GET("/conversations", chatHandler.FindAllConversations)
			chatGroup.GET("/history/:entity_id", chatHandler.FindMessagesHistory)
		}

		// Citas (cliente y negocio; la autorización se resuelve por cita)
		appointments := api.Group("/appointments")
		appointments.Use(middleware.AuthMiddleware(cfg))
		{
			appointments.POST("", agendaHandler.Create)
			appointments.GET("/mine", agendaHandler.Mine)
			appointments.GET("/:id", agendaHandler.Get)
			appointments.POST("/:id/accept", agendaHandler.Accept)
			appointments.POST("/:id/decline", agendaHandler.Decline)
			appointments.POST("/:id/cancel", agendaHandler.Cancel)
			appointments.POST("/:id/outcome", agendaHandler.Outcome)
		}

		// Images (Public Proxy for <img> tags)
		api.GET("/images/:id", mediaHandler.FindMedia)
		imagesProtected := api.Group("/images")
		imagesProtected.Use(middleware.AuthMiddleware(cfg))
		{
			// Subida suelta (devuelve el id del medio): con tope de peso y límite de peticiones por usuario.
			imagesProtected.POST("/upload", middleware.RateLimit(20, time.Minute), mediaHandler.Upload)
		}

		// Users (Protected)
		usersProtected := api.Group("/users")
		usersProtected.Use(middleware.AuthMiddleware(cfg))
		{
			usersProtected.GET("/me", userHandler.FindMe)
			usersProtected.POST("/profile/image", userHandler.UploadProfileImage)
			usersProtected.GET("/me/favorites", favoriteHandler.FindMine)
			usersProtected.GET("/me/novedades", favoriteHandler.Novedades)
			usersProtected.GET("/me/inbox-summary", inboxHandler.Summary)
			usersProtected.POST("/me/novedades/seen", favoriteHandler.MarkNovedadesSeen)
			usersProtected.POST("/push-token", pushHandler.Register)
			usersProtected.DELETE("/push-token", pushHandler.Remove)
		}

		// Asistente de IA para crear negocios (solo dueños autenticados).
		aiGroup := api.Group("/ai")
		aiGroup.Use(middleware.AuthMiddleware(cfg))
		{
			aiGroup.POST("/business-assistant", aiHandler.BusinessAssistant)
			aiGroup.POST("/writing-assistant", aiHandler.ImproveText)
		}

		// Admin (Protected, role=admin only)
		adminGroup := api.Group("/admin")
		adminGroup.Use(middleware.AuthMiddleware(cfg), middleware.RequireAdmin())
		{
			adminGroup.GET("/entities", entityHandler.FindAllByStatus)
			adminGroup.GET("/events/summary", eventHandler.Summary)
			adminGroup.PATCH("/entities/:id/verify", entityHandler.VerifyEntity)

			adminGroup.POST("/categories", categoryHandler.Create)
			adminGroup.PUT("/categories/:id", categoryHandler.Update)
			adminGroup.DELETE("/categories/:id", categoryHandler.Delete)

			adminGroup.POST("/subcategories", categoryHandler.CreateSubcategory)
			adminGroup.PUT("/subcategories/:id", categoryHandler.UpdateSubcategory)
			adminGroup.DELETE("/subcategories/:id", categoryHandler.DeleteSubcategory)
		}

		// Pagos (webhook público: lo llama Wompi, no un usuario autenticado;
		// se protege con la firma de eventos en vez de un JWT).
		paymentsGroup := api.Group("/payments")
		{
			paymentsGroup.POST("/wompi/webhook", paymentHandler.Webhook)
		}

		// Swagger Documentation
		api.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.DefaultModelsExpandDepth(2), ginSwagger.PersistAuthorization(true)))
	}

	// Health Check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	// Start Server
	log.Printf("Server starting on port %s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal("Server start failed: ", err)
	}
}
