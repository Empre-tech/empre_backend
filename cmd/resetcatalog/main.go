// Command resetcatalog wipes every business and every category/subcategory
// from the database, then reseeds the default categories (with their map
// icons) and subcategories. Run it once, manually, from the empre_backend
// folder:
//
//	go run ./cmd/resetcatalog
//
// It uses the same .env / environment variables as the API (cmd/api), so it
// connects to whatever database that is already pointed at. This is
// destructive and unrecoverable: it deletes every business, chat, review and
// favorite along with the old categories. There is no confirmation prompt,
// so only run it when that's really what you want.
package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"empre_backend/config"
	"empre_backend/internal/database"
)

func main() {
	cfg := config.LoadConfig()
	database.ConnectDB(cfg)

	fmt.Printf("Esto borrará TODOS los negocios, categorías y subcategorías de la base de datos %q. Escribe \"si\" para continuar: ", cfg.DBName)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) != "si" {
		fmt.Println("Cancelado, no se borró nada.")
		return
	}

	if err := database.ResetCatalog(database.DB); err != nil {
		log.Fatal("No se pudo limpiar el catálogo: ", err)
	}
	fmt.Println("Negocios y categorías eliminados.")

	database.SeedCategories(database.DB)
	database.SeedSubcategories(database.DB)
	fmt.Println("Listo: categorías y subcategorías predeterminadas creadas de nuevo.")
}
