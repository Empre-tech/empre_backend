package main

import (
	"log"

	"empre_backend/config"
	"empre_backend/internal/database"
	"empre_backend/internal/handlers"
	"empre_backend/internal/middleware"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/internal/services"
	"empre_backend/internal/websocket"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load Config
	cfg := config.LoadConfig()

	// Connect to Database
	database.ConnectDB(cfg)

	// Auto Migrate
	err := database.DB.AutoMigrate(&models.User{}, &models.Entity{}, &models.Message{}, &models.Category{}, &models.Media{}, &models.EntityPhoto{})
	if err != nil {
		log.Fatal("Migration failed: ", err)
	}

	// Initialize Router
	r := gin.Default()

	// Initialize Services & Handlers
	// Auth
	authService := services.NewAuthService(database.DB, cfg)
	authHandler := handlers.NewAuthHandler(authService)

	// Storage & Media
	storageService := services.NewStorageService(cfg)
	mediaHandler := handlers.NewMediaHandler(storageService, database.DB)

	// Entity
	entityRepo := repository.NewEntityRepository(database.DB)
	entityService := services.NewEntityService(entityRepo)
	entityHandler := handlers.NewEntityHandler(entityService, storageService, database.DB)

	// Category
	categoryRepo := repository.NewCategoryRepository(database.DB)
	categoryService := services.NewCategoryService(categoryRepo)
	categoryHandler := handlers.NewCategoryHandler(categoryService)

	// Chat / WebSocket
	chatRepo := repository.NewChatRepository(database.DB)
	chatService := services.NewChatService(chatRepo)
	wsHub := websocket.NewHub(database.DB)
	go wsHub.Run()
	chatHandler := handlers.NewChatHandler(wsHub, chatService)

	// Routes
	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
		}

		entities := api.Group("/entities")
		entities.GET("", entityHandler.FindAll)
		entities.GET("/:id", entityHandler.FindByID)

		categories := api.Group("/categories")
		categories.GET("", categoryHandler.FindAll)
		categories.GET("/:id", categoryHandler.FindByID)
		categories.POST("", categoryHandler.Create)
		categories.PUT("/:id", categoryHandler.Update)
		categories.DELETE("/:id", categoryHandler.Delete)

		// Protected Routes
		entitiesProtected := entities.Use(middleware.AuthMiddleware(cfg))
		{
			entitiesProtected.POST("", entityHandler.Create)
			entitiesProtected.GET("/mine", entityHandler.FindAllByOwner)
			entitiesProtected.PUT("/:id", entityHandler.Update)
			entitiesProtected.DELETE("/:id", entityHandler.Delete)
			entitiesProtected.POST("/:id/images", entityHandler.UploadImage)
		}

		// WebSocket & Chat History
		chatGroup := api.Group("/chat")
		chatGroup.Use(middleware.AuthMiddleware(cfg))
		{
			chatGroup.GET("/ws", chatHandler.HandleWebSocket)
			chatGroup.GET("/conversations", chatHandler.FindAllConversations)
			chatGroup.GET("/history/:entity_id", chatHandler.FindMessagesHistory)
		}

		// Images (Public Proxy + Protected Upload)
		api.GET("/images/:id", mediaHandler.FindMedia)
		imagesProtected := api.Group("/images")
		imagesProtected.Use(middleware.AuthMiddleware(cfg))
		{
			imagesProtected.POST("/upload", mediaHandler.Upload)
		}
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
