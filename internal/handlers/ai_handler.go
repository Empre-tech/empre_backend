package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type AIHandler struct {
	AI          *services.AIService
	CategorySvc *services.CategoryService
}

func NewAIHandler(ai *services.AIService, categorySvc *services.CategoryService) *AIHandler {
	return &AIHandler{AI: ai, CategorySvc: categorySvc}
}

type AIChatRequest struct {
	Messages []services.AIMessage `json:"messages" binding:"required"`
}

// AIBusinessDraft is what the model can propose via the propose_business_draft
// tool. Every field is optional: the model fills in what it already knows and
// leaves the rest for a later turn (or for the owner to fill by hand).
type AIBusinessDraft struct {
	NameSuggestions []string      `json:"name_suggestions,omitempty"`
	Description     string        `json:"description,omitempty"`
	CategoryID      string        `json:"category_id,omitempty"`
	SubcategoryIDs  []string      `json:"subcategory_ids,omitempty"`
	ServiceMode     string        `json:"service_mode,omitempty"`
	Hours           []HourRequest `json:"hours,omitempty"`
	Ready           bool          `json:"ready,omitempty"`
}

type AIChatResponse struct {
	Reply string           `json:"reply"`
	Draft *AIBusinessDraft `json:"draft,omitempty"`
	Ready bool             `json:"ready"`
}

const businessDraftToolName = "propose_business_draft"

var businessDraftToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"name_suggestions": {
			"type": "array",
			"items": {"type": "string"},
			"description": "1 a 3 nombres sugeridos para el negocio, solo si el dueño todavía no tiene uno decidido."
		},
		"description": {
			"type": "string",
			"description": "Descripción atractiva del negocio para su perfil, 2 a 4 frases, en español, tono cálido."
		},
		"category_id": {
			"type": "string",
			"description": "UUID exacto de una categoría de la lista que te dieron. Nunca inventes un UUID."
		},
		"subcategory_ids": {
			"type": "array",
			"items": {"type": "string"},
			"description": "UUIDs exactos de subcategorías de esa categoría, de la lista que te dieron."
		},
		"service_mode": {
			"type": "string",
			"enum": ["in_place", "delivery", "both"],
			"description": "Cómo presta el servicio el negocio."
		},
		"hours": {
			"type": "array",
			"description": "Horario por día de la semana (0=domingo ... 6=sábado). Incluye los 7 días si el dueño ya te dio el horario completo.",
			"items": {
				"type": "object",
				"properties": {
					"weekday": {"type": "integer", "minimum": 0, "maximum": 6},
					"closed": {"type": "boolean"},
					"is_24h": {"type": "boolean"},
					"open_time": {"type": "string", "description": "Formato HH:MM, 24 horas."},
					"close_time": {"type": "string", "description": "Formato HH:MM, 24 horas."}
				},
				"required": ["weekday"]
			}
		},
		"ready": {
			"type": "boolean",
			"description": "true solo cuando ya tienes categoría, horario y modalidad (el nombre puede quedar como sugerencias) y el dueño puede pasar a revisar el formulario."
		}
	}
}`)

const aiSystemPromptTemplate = `Eres el asistente de creación de negocios de Empre, una app para descubrir negocios en Cartagena, Colombia. Estás ayudando al DUEÑO de un negocio a crear su perfil conversando con él en español, de forma cálida, breve y cercana (como alguien de Cartagena), nunca con listas ni formato de encuesta.

Tu meta es reunir, con preguntas cortas (una o dos a la vez, nunca un cuestionario largo):
- Nombre del negocio (si no lo tiene decidido, sugiere 1-3 opciones basadas en lo que cuenta).
- Una descripción atractiva para su perfil (tú la escribes con base en lo que él te cuenta, no le preguntes "escribe tu descripción").
- Categoría y subcategoría: SOLO puedes usar las de esta lista exacta (usa sus IDs tal cual, nunca inventes uno):
%s
- Modalidad de servicio: en el lugar, a domicilio, o ambos.
- Horario de atención por día de la semana (si abre igual todos los días, con eso basta; si varía, pregunta las excepciones, no los 7 días uno por uno).

NO preguntes por dirección, ubicación en el mapa, fotos, ni datos de contacto: eso se llena aparte en la app.

Cada vez que tengas información nueva o actualizada, llama a la herramienta "propose_business_draft" con los campos que ya tengas claros (puedes llamarla varias veces a lo largo de la conversación, no solo al final). Cuando ya tengas categoría, horario y modalidad, marca "ready": true en esa llamada y dile al dueño que puede revisar el formulario. Responde siempre también con un mensaje de texto breve para el dueño, además de la llamada a la herramienta.`

func (h *AIHandler) BusinessAssistant(c *gin.Context) {
	if h.AI == nil || !h.AI.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "El asistente de IA no está disponible en este momento."})
		return
	}

	var req AIChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages no puede estar vacío"})
		return
	}

	categories, _, err := h.CategorySvc.FindAll(1, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos cargar las categorías."})
		return
	}

	validCategoryIDs := map[string]bool{}
	validSubcategoryIDs := map[string]bool{}
	catalog := make([]map[string]any, 0, len(categories))
	for _, cat := range categories {
		validCategoryIDs[cat.ID.String()] = true
		subs := make([]map[string]any, 0, len(cat.Subcategories))
		for _, sub := range cat.Subcategories {
			validSubcategoryIDs[sub.ID.String()] = true
			subs = append(subs, map[string]any{"id": sub.ID.String(), "name": sub.Name})
		}
		catalog = append(catalog, map[string]any{
			"id":            cat.ID.String(),
			"name":          cat.Name,
			"subcategories": subs,
		})
	}
	catalogJSON, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos preparar el catálogo de categorías."})
		return
	}

	systemPrompt := fmt.Sprintf(aiSystemPromptTemplate, string(catalogJSON))

	tools := []services.AITool{{
		Name:        businessDraftToolName,
		Description: "Guarda o actualiza el borrador del negocio con la información recopilada hasta ahora en la conversación.",
		InputSchema: businessDraftToolSchema,
	}}

	reply, err := h.AI.Chat(systemPrompt, req.Messages, tools)
	if err != nil {
		// El detalle real (401 de API key inválida, endpoint/modelo mal escrito,
		// etc.) solo importa para depurar desde la consola del backend; al
		// dueño nunca le mostramos el error crudo del proveedor de IA.
		log.Println("ai: business-assistant error:", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "No pudimos hablar con el asistente. Intenta de nuevo en un momento."})
		return
	}

	resp := AIChatResponse{Reply: reply.Text}
	for _, call := range reply.ToolCalls {
		if call.Name != businessDraftToolName {
			continue
		}
		var draft AIBusinessDraft
		if err := json.Unmarshal(call.Input, &draft); err != nil {
			continue
		}
		// El modelo puede alucinar un UUID que no existe en el catálogo real:
		// nunca dejamos pasar una categoría/subcategoría inválida al frontend.
		if draft.CategoryID != "" && !validCategoryIDs[draft.CategoryID] {
			draft.CategoryID = ""
		}
		if len(draft.SubcategoryIDs) > 0 {
			filtered := make([]string, 0, len(draft.SubcategoryIDs))
			for _, id := range draft.SubcategoryIDs {
				if validSubcategoryIDs[id] {
					filtered = append(filtered, id)
				}
			}
			draft.SubcategoryIDs = filtered
		}
		if draft.ServiceMode != "in_place" && draft.ServiceMode != "delivery" && draft.ServiceMode != "both" {
			draft.ServiceMode = ""
		}
		draft.Hours = validAIHours(draft.Hours)

		if resp.Draft == nil {
			resp.Draft = &draft
		} else {
			mergeAIDraft(resp.Draft, &draft)
		}
		if draft.Ready {
			resp.Ready = true
		}
	}

	c.JSON(http.StatusOK, resp)
}

// mergeAIDraft folds a later tool call's fields into the first one so the
// response always carries the latest value the model gave for each field
// (the model calling the tool more than once in the same turn is rare, but
// cheap to handle correctly).
func mergeAIDraft(dst, src *AIBusinessDraft) {
	if len(src.NameSuggestions) > 0 {
		dst.NameSuggestions = src.NameSuggestions
	}
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.CategoryID != "" {
		dst.CategoryID = src.CategoryID
	}
	if len(src.SubcategoryIDs) > 0 {
		dst.SubcategoryIDs = src.SubcategoryIDs
	}
	if src.ServiceMode != "" {
		dst.ServiceMode = src.ServiceMode
	}
	if len(src.Hours) > 0 {
		dst.Hours = src.Hours
	}
	if src.Ready {
		dst.Ready = true
	}
}

// AITextRequest is a request to improve/generate a short piece of copy (a
// business description or a post caption) — much simpler than the
// conversational assistant: one call in, a few ready-to-use options out.
type AITextRequest struct {
	// Kind selects which prompt we use: "business_description" or "post_caption".
	Kind string `json:"kind" binding:"required"`
	// CurrentText is whatever the owner already wrote, if anything. May be empty.
	CurrentText string `json:"current_text"`
	// Context the model can use to make the suggestion specific instead of
	// generic — all optional.
	BusinessName string `json:"business_name"`
	CategoryName string `json:"category_name"`
}

type AITextResponse struct {
	Suggestions []string `json:"suggestions"`
}

const textSuggestionsToolName = "propose_text_suggestions"

var textSuggestionsToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"suggestions": {
			"type": "array",
			"items": {"type": "string"},
			"minItems": 2,
			"maxItems": 3,
			"description": "2 o 3 opciones de texto, cada una lista para usar tal cual, sin numerarlas ni explicarlas."
		}
	},
	"required": ["suggestions"]
}`)

const aiTextSystemPromptBusinessDescription = `Eres un redactor publicitario ayudando a un dueño de negocio en Cartagena, Colombia a escribir la descripción de su perfil en Empre (una app para descubrir negocios). Escribe en español, con un tono cálido y cercano, nunca genérico ni robótico. Cada opción debe tener entre 2 y 4 frases, describir qué hace especial al negocio y dar ganas de visitarlo. No inventes datos concretos (precios, horarios, direcciones) que no te dieron.

MUY IMPORTANTE: el dueño no puede responderte preguntas, esto es una sola llamada sin conversación. Nunca respondas pidiendo más información ni hagas preguntas: con lo que te dieron (aunque sea solo el nombre del negocio, o incluso nada) escribe igual 2 o 3 opciones completas y usables, apoyándote en el nombre y/o la categoría si los tienes, o de forma genérica pero cálida si no tienes nada. Siempre llama a la herramienta "propose_text_suggestions" con 2 o 3 opciones distintas entre sí (no variaciones mínimas de la misma frase); nunca respondas solo con texto plano.`

const aiTextSystemPromptPostCaption = `Eres un redactor de redes sociales ayudando a un dueño de negocio en Cartagena, Colombia a escribir el texto (caption) de una publicación de fotos/video en Empre, una app estilo Instagram para descubrir negocios. Escribe en español, corto y llamativo (1 a 2 frases, máximo ~150 caracteres cada opción), con un tono cercano y cartagenero. No inventes datos concretos que no te dieron.

MUY IMPORTANTE: el dueño no puede responderte preguntas, esto es una sola llamada sin conversación. Nunca respondas pidiendo más información ni hagas preguntas: con lo que te dieron (aunque sea poco o nada) escribe igual 2 o 3 opciones completas y usables, genéricas pero atractivas si no tienes contexto. Siempre llama a la herramienta "propose_text_suggestions" con 2 o 3 opciones distintas entre sí; nunca respondas solo con texto plano.`

// ImproveText generates a few ready-to-use options for a business description
// or a post caption. Unlike BusinessAssistant this is a single request/response
// (no conversation history): the owner picks one option or asks again.
func (h *AIHandler) ImproveText(c *gin.Context) {
	if h.AI == nil || !h.AI.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "El asistente de IA no está disponible en este momento."})
		return
	}

	var req AITextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var systemPrompt string
	switch req.Kind {
	case "business_description":
		systemPrompt = aiTextSystemPromptBusinessDescription
	case "post_caption":
		systemPrompt = aiTextSystemPromptPostCaption
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind debe ser business_description o post_caption"})
		return
	}

	userMsg := "Genera las opciones."
	var details []string
	if req.BusinessName != "" {
		details = append(details, fmt.Sprintf("Negocio: %s", req.BusinessName))
	}
	if req.CategoryName != "" {
		details = append(details, fmt.Sprintf("Categoría: %s", req.CategoryName))
	}
	if req.CurrentText != "" {
		details = append(details, fmt.Sprintf("Texto actual del dueño (mejóralo o inspírate en él, no lo repitas igual): %s", req.CurrentText))
	}
	if len(details) > 0 {
		userMsg = fmt.Sprintf("%s\n\n%s", userMsg, strings.Join(details, "\n"))
	}

	messages := []services.AIMessage{{Role: services.AIRoleUser, Content: userMsg}}
	tools := []services.AITool{{
		Name:        textSuggestionsToolName,
		Description: "Entrega las opciones de texto generadas.",
		InputSchema: textSuggestionsToolSchema,
	}}

	reply, err := h.AI.Chat(systemPrompt, messages, tools)
	if err != nil {
		log.Println("ai: improve-text error:", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "No pudimos generar sugerencias. Intenta de nuevo en un momento."})
		return
	}

	var resp AITextResponse
	for _, call := range reply.ToolCalls {
		if call.Name != textSuggestionsToolName {
			continue
		}
		var parsed AITextResponse
		if err := json.Unmarshal(call.Input, &parsed); err != nil {
			continue
		}
		resp.Suggestions = parsed.Suggestions
	}
	if len(resp.Suggestions) == 0 {
		// El modelo no llamó a la herramienta (p. ej. respondió con una
		// pregunta en vez de generar opciones). El dueño no puede contestarle
		// preguntas en este flujo de una sola llamada, así que en vez de
		// mostrar ese texto como si fuera una opción usable, es mejor pedirle
		// que reintente (el botón "Reintentar" ya existe en el frontend).
		log.Println("ai: improve-text no devolvió sugerencias; texto del modelo:", reply.Text)
		c.JSON(http.StatusBadGateway, gin.H{"error": "No pudimos generar sugerencias. Intenta de nuevo."})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func validAIHours(rows []HourRequest) []HourRequest {
	out := make([]HourRequest, 0, len(rows))
	for _, r := range rows {
		if r.Weekday < 0 || r.Weekday > 6 {
			continue
		}
		out = append(out, r)
	}
	return out
}
