// Command resetall wipes the database back to a blank slate: every business
// and everything hanging off it (chats, reviews, favorites, photos, posts,
// hours, subscriptions), the catalog (categories/subcategories), and every
// user account that is NOT an admin (models.RoleAdmin) — along with that
// user's own tokens. Admin accounts survive untouched, and the default
// categories/subcategories get reseeded automatically the next time the API
// starts (cmd/api already calls SeedCategories/SeedSubcategories on an empty
// table).
//
// Run it once, manually, from the empre_backend folder:
//
//	go run ./cmd/resetall
//
// It uses the same .env / environment variables as the API (cmd/api), so it
// connects to whatever database that is already pointed at — including the
// real Supabase database if that's what DB_HOST points to. This is
// destructive and unrecoverable. It asks for confirmation before doing
// anything.
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

	fmt.Printf(
		"Esto borrará TODOS los negocios, chats, reseñas, categorías Y toda cuenta de usuario que no sea admin, de la base de datos %q.\nLas cuentas con role=admin se conservan. Escribe \"si\" para continuar: ",
		cfg.DBName,
	)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) != "si" {
		fmt.Println("Cancelado, no se borró nada.")
		return
	}

	if err := database.ResetEverythingExceptAdmins(database.DB); err != nil {
		log.Fatal("No se pudo reiniciar la base de datos: ", err)
	}
	fmt.Println("Listo: negocios, catálogo y usuarios no-admin eliminados.")

	database.SeedCategories(database.DB)
	database.SeedSubcategories(database.DB)
	fmt.Println("Categorías y subcategorías predeterminadas creadas de nuevo.")
}
