package main

import (
	"context"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 1. DATABASE MODELS (SCHEMAS)

type Repository struct {
	gorm.Model
	Name        string `json:"name" gorm:"type:varchar(100);not null;unique"`
	Description string `json:"description" gorm:"type:text"`
	IsPrivate   bool   `json:"is_private" gorm:"default:false"`
	CloneURL    string `json:"clone_url"`
}

type Student struct {
	gorm.Model
	StudentID string `json:"student_id" gorm:"type:varchar(50);not null;unique;index"`
	Name      string `json:"name" gorm:"type:varchar(100);not null"`
	Branch    string `json:"branch" gorm:"type:varchar(100);not null"`
	Year      int    `json:"year" gorm:"type:integer;not null"`
}

var DB *gorm.DB

type RenameRepoPayload struct {
	NewName string `json:"new_name"`
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Fallback to your local setup for laptop development testing
		dsn = "host=localhost user=postgres password=secret dbname=startup_lms port=5432 sslmode=disable"
	}
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Automate table generation inside pgAdmin for both structures
	DB.AutoMigrate(&Repository{}, &Student{})

	app := fiber.New()

	// 3. REPOSITORY CRUD ROUTES
	repoGroup := app.Group("/api/v1/repos")

	// CREATE Repo Tracker
	repoGroup.Post("/", func(c *fiber.Ctx) error {
		repo := new(Repository)
		if err := c.BodyParser(repo); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid JSON payload"})
		}
		if err := DB.Create(&repo).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusCreated).JSON(repo)
	})

	// READ ALL Repos
	repoGroup.Get("/", func(c *fiber.Ctx) error {
		var repos []Repository
		DB.Find(&repos)
		return c.Status(fiber.StatusOK).JSON(repos)
	})

	// READ ONE Repo
	repoGroup.Get("/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var repo Repository
		if err := DB.First(&repo, id).Error; err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Repository not found"})
		}
		return c.Status(fiber.StatusOK).JSON(repo)
	})

	// UPDATE Repo Metadata Locally
	repoGroup.Put("/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var repo Repository
		if err := DB.First(&repo, id).Error; err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Repository not found"})
		}
		updateData := new(Repository)
		if err := c.BodyParser(updateData); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid update JSON"})
		}
		DB.Model(&repo).Updates(updateData)
		return c.Status(fiber.StatusOK).JSON(repo)
	})

	// DELETE Repo Tracker
	repoGroup.Delete("/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var repo Repository
		if err := DB.First(&repo, id).Error; err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Repository not found"})
		}
		DB.Delete(&repo)
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Repository tracking entry soft-deleted"})
	})

	// THE ORCHESTRATION ROUTE: Sync & Change Repo Name on GitHub + Database
	repoGroup.Patch("/:id/rename", func(c *fiber.Ctx) error {
		id := c.Params("id")
		githubToken := os.Getenv("GITHUB_ACCESS_TOKEN")
		if githubToken == "" {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Missing GITHUB_ACCESS_TOKEN env variable"})
		}

		payload := new(RenameRepoPayload)
		if err := c.BodyParser(payload); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
		}
		if payload.NewName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "new_name field is required"})
		}

		// 1. Fetch old repository name from Postgres
		var repo Repository
		if err := DB.First(&repo, id).Error; err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Repository tracking record not found"})
		}
		oldName := repo.Name

		// 2. Call external GitHub REST API to execute the physical rename
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: githubToken})
		tc := oauth2.NewClient(ctx, ts)
		githubClient := github.NewClient(tc)

		githubConfig := &github.Repository{Name: github.String(payload.NewName)}
		updatedGitHub, resp, err := githubClient.Repositories.Edit(ctx, "PraveenBairagee9755", oldName, githubConfig)
		if err != nil {
			return c.Status(resp.StatusCode).JSON(fiber.Map{"error": "GitHub execution failed", "details": err.Error()})
		}

		// 3. Update the local PostgreSQL database row with newly returned assets
		repo.Name = updatedGitHub.GetName()
		repo.CloneURL = updatedGitHub.GetCloneURL()
		DB.Save(&repo)

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"message":       "Successfully orchestrated GitHub rename and database synchronization!",
			"old_repo_name": oldName,
			"new_repo_name": repo.Name,
			"new_clone_url": repo.CloneURL,
		})
	})

	// 4. STUDENT CRUD ROUTES
	
	studentGroup := app.Group("/api/v1/students")

	// CREATE Student
	studentGroup.Post("/", func(c *fiber.Ctx) error {
		student := new(Student)
		if err := c.BodyParser(student); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input formatting"})
		}
		if err := DB.Create(&student).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusCreated).JSON(student)
	})

	// READ ALL Students
	studentGroup.Get("/", func(c *fiber.Ctx) error {
		var students []Student
		DB.Find(&students)
		return c.Status(fiber.StatusOK).JSON(students)
	})

	// UPDATE Student via Custom ID
	studentGroup.Put("/:student_id", func(c *fiber.Ctx) error {
		searchID := c.Params("student_id")
		var student Student
		if err := DB.Where("student_id = ?", searchID).First(&student).Error; err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Student not found"})
		}
		updateData := new(Student)
		if err := c.BodyParser(updateData); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid JSON structure"})
		}
		DB.Model(&student).Updates(updateData)
		return c.Status(fiber.StatusOK).JSON(student)
	})

	// DELETE Student via Custom ID
	studentGroup.Delete("/:student_id", func(c *fiber.Ctx) error {
		searchID := c.Params("student_id")
		var student Student
		if err := DB.Where("student_id = ?", searchID).First(&student).Error; err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Student not found"})
		}
		DB.Delete(&student)
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Student soft-deleted successfully"})
	})

	log.Fatal(app.Listen(":8080"))
}
