// @title Empre Backend API
// @version 1.0
// @description Interactive API documentation for the Local Discovery App.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /
// @query.collection.format multi

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer <your-token>" to authenticate.
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

	_ "empre_backend/docs"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

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

	// Initialize Repositories
	userRepo := repository.NewUserRepository(database.DB)
	categoryRepo := repository.NewCategoryRepository(database.DB)
	entityRepo := repository.NewEntityRepository(database.DB)
	mediaRepo := repository.NewMediaRepository(database.DB)
	chatRepo := repository.NewChatRepository(database.DB)

	// Initialize Services
	storageService := services.NewStorageService(cfg)
	mediaService := services.NewMediaService(mediaRepo, storageService, cfg.AppURL)
	authService := services.NewAuthService(userRepo, cfg)
	userService := services.NewUserService(userRepo, mediaService)
	entityService := services.NewEntityService(entityRepo, mediaService)
	categoryService := services.NewCategoryService(categoryRepo)
	chatService := services.NewChatService(chatRepo)

	// Initialize Handlers
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService, mediaService)
	mediaHandler := handlers.NewMediaHandler(mediaService)
	entityHandler := handlers.NewEntityHandler(entityService, mediaService, database.DB)
	categoryHandler := handlers.NewCategoryHandler(categoryService)

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

		// Users (Protected)
		usersProtected := api.Group("/users")
		usersProtected.Use(middleware.AuthMiddleware(cfg))
		{
			usersProtected.GET("/me", userHandler.FindMe)
			usersProtected.POST("/profile/image", userHandler.UploadProfileImage)
		}

		// Swagger Documentation
		api.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
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
