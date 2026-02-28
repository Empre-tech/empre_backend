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
	err := database.DB.AutoMigrate(
		&models.User{},
		&models.Entity{},
		&models.Message{},
		&models.Category{},
		&models.Media{},
		&models.EntityPhoto{},
		&models.PasswordResetToken{},
		&models.RefreshToken{},
	)
	if err != nil {
		log.Fatal("Migration failed: ", err)
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
			auth.POST("/refresh", authHandler.RefreshToken)
			auth.POST("/password-reset/request", authHandler.RequestPasswordReset)
			auth.POST("/password-reset/reset", authHandler.ResetPassword)
		}

		entities := api.Group("/entities")
		{
			// Public viewing (Discovery)
			entities.GET("", entityHandler.FindAll)
			entities.GET("/:id", entityHandler.FindByID)

			// Protected mutations
			entitiesProtected := entities.Use(middleware.AuthMiddleware(cfg))
			{
				entitiesProtected.POST("", entityHandler.Create)
				entitiesProtected.GET("/mine", entityHandler.FindAllByOwner)
				entitiesProtected.PUT("/:id", entityHandler.Update)
				entitiesProtected.DELETE("/:id", entityHandler.Delete)
				entitiesProtected.POST("/:id/images", entityHandler.UploadImage)
			}
		}

		categories := api.Group("/categories")
		{
			// Public viewing
			categories.GET("", categoryHandler.FindAll)
			categories.GET("/:id", categoryHandler.FindByID)

			// Protected mutations
			categoriesProtected := categories.Use(middleware.AuthMiddleware(cfg))
			{
				categoriesProtected.POST("", categoryHandler.Create)
				categoriesProtected.PUT("/:id", categoryHandler.Update)
				categoriesProtected.DELETE("/:id", categoryHandler.Delete)
			}
		}

		// WebSocket & Chat History
		chatGroup := api.Group("/chat")
		chatGroup.Use(middleware.AuthMiddleware(cfg))
		{
			chatGroup.GET("/ws", chatHandler.HandleWebSocket)
			chatGroup.GET("/conversations", chatHandler.FindAllConversations)
			chatGroup.GET("/history/:entity_id", chatHandler.FindMessagesHistory)
		}

		// Images (Public Proxy for <img> tags)
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
