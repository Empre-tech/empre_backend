package handlers

import (
	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CreateCategoryRequest struct {
	Name string `json:"name" binding:"required"`
	Icon string `json:"icon" binding:"required"`
}

type UpdateCategoryRequest struct {
	Name string `json:"name" binding:"required"`
	Icon string `json:"icon" binding:"required"`
}

type CategoryHandler struct {
	categoryService *services.CategoryService
}

func NewCategoryHandler(categoryService *services.CategoryService) *CategoryHandler {
	return &CategoryHandler{categoryService: categoryService}
}

// Create handles category creation
// @Summary Create a new category
// @Description Register a new category for business entities (Admin only)
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateCategoryRequest true "Category Info"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/admin/categories [post]
func (h *CategoryHandler) Create(c *gin.Context) {
	var req CreateCategoryRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	category := models.Category{
		Name: req.Name,
		Icon: req.Icon,
	}

	if err := h.categoryService.Create(&category); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Category created successfully", "id": category.ID})
}

// FindAll retrieves all categories with pagination
// @Summary Find all categories
// @Description Get a paginated list of all business categories
// @Tags Categories
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param pageSize query int false "Items per page" default(20)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /api/categories [get]
func (h *CategoryHandler) FindAll(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	categories, total, err := h.categoryService.FindAll(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var response []dtos.CategoryResponse
	for _, cat := range categories {
		response = append(response, dtos.CategoryResponse{
			ID:            cat.ID,
			Name:          cat.Name,
			Icon:          cat.Icon,
			Subcategories: subcategoryDTOs(cat.Subcategories),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": response,
		"meta": gin.H{
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

func subcategoryDTOs(subs []models.Subcategory) []dtos.SubcategoryResponse {
	if len(subs) == 0 {
		return nil
	}
	out := make([]dtos.SubcategoryResponse, 0, len(subs))
	for _, sc := range subs {
		out = append(out, dtos.SubcategoryResponse{ID: sc.ID, Name: sc.Name, CategoryID: sc.CategoryID})
	}
	return out
}

// FindByID retrieves a category by its UUID
// @Summary Find category by ID
// @Description Get details of a specific business category
// @Tags Categories
// @Produce json
// @Param id path string true "Category ID"
// @Success 200 {object} dtos.CategoryResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/categories/{id} [get]
func (h *CategoryHandler) FindByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	category, err := h.categoryService.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	response := dtos.CategoryResponse{
		ID:            category.ID,
		Name:          category.Name,
		Icon:          category.Icon,
		Subcategories: subcategoryDTOs(category.Subcategories),
	}

	c.JSON(http.StatusOK, response)
}

// Update modifies an existing category
// @Summary Update category
// @Description Update the name/icon of a business category (Admin only)
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Category ID"
// @Param request body UpdateCategoryRequest true "Update Info"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/admin/categories/{id} [put]
func (h *CategoryHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	var req UpdateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	category, err := h.categoryService.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}
	category.Name = req.Name
	category.Icon = req.Icon

	if err := h.categoryService.Update(category); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category updated successfully"})
}

// Delete removes an existing category
// @Summary Delete category
// @Description Remove a specific business category (Admin only)
// @Tags Categories
// @Produce json
// @Security BearerAuth
// @Param id path string true "Category ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/admin/categories/{id} [delete]
func (h *CategoryHandler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	category := models.Category{
		ID: id,
	}

	if err := h.categoryService.Delete(&category); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted successfully"})
}

type CreateSubcategoryRequest struct {
	Name       string `json:"name" binding:"required"`
	CategoryID string `json:"category_id" binding:"required"`
}

type UpdateSubcategoryRequest struct {
	Name string `json:"name" binding:"required"`
}

// CreateSubcategory adds a subcategory to a category.
// @Summary Create a subcategory
// @Description Add a subcategory under a category (Admin only)
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateSubcategoryRequest true "Subcategory Info"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/admin/subcategories [post]
func (h *CategoryHandler) CreateSubcategory(c *gin.Context) {
	var req CreateSubcategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	categoryID, err := uuid.Parse(req.CategoryID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	sub := models.Subcategory{Name: req.Name, CategoryID: categoryID}
	if err := h.categoryService.CreateSubcategory(&sub); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Subcategory created successfully", "id": sub.ID})
}

// UpdateSubcategory renames a subcategory.
// @Summary Update a subcategory
// @Description Rename a subcategory (Admin only)
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Subcategory ID"
// @Param request body UpdateSubcategoryRequest true "Update Info"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/admin/subcategories/{id} [put]
func (h *CategoryHandler) UpdateSubcategory(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	var req UpdateSubcategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sub, err := h.categoryService.FindSubcategoryByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subcategory not found"})
		return
	}
	sub.Name = req.Name

	if err := h.categoryService.UpdateSubcategory(sub); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Subcategory updated successfully"})
}

// DeleteSubcategory removes a subcategory.
// @Summary Delete a subcategory
// @Description Remove a subcategory (Admin only)
// @Tags Categories
// @Produce json
// @Security BearerAuth
// @Param id path string true "Subcategory ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/admin/subcategories/{id} [delete]
func (h *CategoryHandler) DeleteSubcategory(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	sub := models.Subcategory{ID: id}
	if err := h.categoryService.DeleteSubcategory(&sub); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Subcategory deleted successfully"})
}
