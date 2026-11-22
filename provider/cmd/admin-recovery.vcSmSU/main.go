package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"infraflow/provider/internal/adapters/sqlite"
	"infraflow/provider/internal/application"
	"infraflow/provider/internal/domain"
)

func main() {
	ctx := context.Background()

	db, err := sqlite.New("/var/lib/infraflow-provider/.infraflow-users.sqlite3")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	auth, err := application.NewAuthenticator(db, 8*time.Hour)
	if err != nil {
		panic(err)
	}

	users, err := auth.ListUsers(ctx)
	if err != nil {
		panic(err)
	}

	var admin *domain.User
	for i := range users {
		if strings.EqualFold(users[i].Username, "admin") {
			admin = &users[i]
			break
		}
	}

	if admin == nil {
		panic("compte admin introuvable : aucune modification effectuée")
	}
	if admin.Role != domain.UserRoleAdmin || admin.Disabled {
		panic("le compte admin n'est pas un administrateur actif")
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	password := base64.RawURLEncoding.EncodeToString(random)

	// Préparer le nouveau credential avant la mise à jour.
	tmp := "/var/lib/infraflow-provider/.infraflow-admin-password.new"
	if err := os.WriteFile(tmp, []byte(password+"\n"), 0600); err != nil {
		panic(err)
	}

	if _, err := auth.UpdateUser(ctx, *admin, password); err != nil {
		_ = os.Remove(tmp)
		panic(err)
	}

	if err := os.Rename(tmp, "/var/lib/infraflow-provider/.infraflow-admin-password"); err != nil {
		fmt.Println("ATTENTION : hash mis à jour, mais fichier credential non remplacé.")
		fmt.Println("Nouveau mot de passe à conserver immédiatement :", password)
		panic(err)
	}

	fmt.Println("Réinitialisation réussie.")
	fmt.Println("Utilisateur : admin")
	fmt.Println("Credential mis à jour.")
	fmt.Println("Anciennes sessions administrateur révoquées.")
}
