package frontend

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	apiv1 "github.com/usememos/memos/api/v1"
	"github.com/usememos/memos/internal/util"
	"github.com/usememos/memos/plugin/gomark/parser"
	"github.com/usememos/memos/plugin/gomark/parser/tokenizer"
	"github.com/usememos/memos/plugin/gomark/renderer"
	"github.com/usememos/memos/server/profile"
	"github.com/usememos/memos/store"
)

const (
	// maxMetadataDescriptionLength is the maximum length of metadata description.
	maxMetadataDescriptionLength = 256
)

type FrontendService struct {
	Profile *profile.Profile
	Store   *store.Store
}

func NewFrontendService(profile *profile.Profile, store *store.Store) *FrontendService {
	return &FrontendService{
		Profile: profile,
		Store:   store,
	}
}

func (s *FrontendService) Serve(ctx context.Context, e *echo.Echo) {
	// Use echo static middleware to serve the built dist folder.
	// refer: https://github.com/labstack/echo/blob/master/middleware/static.go
	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Root:  "dist",
		HTML5: true,
		Skipper: func(c echo.Context) bool {
			return util.HasPrefixes(c.Path(), "/api", "/memos.api.v2", "/robots.txt", "/sitemap.xml", "/m/:memoID")
		},
	}))

	s.registerRoutes(e)
	s.registerFileRoutes(ctx, e)
}

func (s *FrontendService) registerRoutes(e *echo.Echo) {
	rawIndexHTML := getRawIndexHTML()

	e.GET("/m/:memoID", func(c echo.Context) error {
		ctx := c.Request().Context()
		memoID, err := util.ConvertStringToInt32(c.Param("memoID"))
		if err != nil {
			// Redirect to `index.html` if any error occurs.
			return c.HTML(http.StatusOK, rawIndexHTML)
		}

		memo, err := s.Store.GetMemo(ctx, &store.FindMemo{
			ID: &memoID,
		})
		if err != nil {
			return c.HTML(http.StatusOK, rawIndexHTML)
		}
		if memo == nil {
			return c.HTML(http.StatusOK, rawIndexHTML)
		}
		creator, err := s.Store.GetUser(ctx, &store.FindUser{
			ID: &memo.CreatorID,
		})
		if err != nil {
			return c.HTML(http.StatusOK, rawIndexHTML)
		}

		// Inject memo metadata into `index.html`.
		indexHTML := strings.ReplaceAll(rawIndexHTML, "<!-- memos.metadata -->", generateMemoMetadata(memo, creator))
		return c.HTML(http.StatusOK, indexHTML)
	})
}

func (s *FrontendService) registerFileRoutes(ctx context.Context, e *echo.Echo) {
	instanceURLSetting, err := s.Store.GetSystemSetting(ctx, &store.FindSystemSetting{
		Name: apiv1.SystemSettingInstanceURLName.String(),
	})
	if err != nil || instanceURLSetting == nil {
		return
	}
	instanceURL := instanceURLSetting.Value
	if instanceURL == "" {
		return
	}

	e.GET("/robots.txt", func(c echo.Context) error {
		robotsTxt := fmt.Sprintf(`User-agent: *
Allow: /
Host: %s
Sitemap: %s/sitemap.xml`, instanceURL, instanceURL)
		return c.String(http.StatusOK, robotsTxt)
	})

	e.GET("/sitemap.xml", func(c echo.Context) error {
		ctx := c.Request().Context()
		urlsets := []string{}
		// Append memo list.
		memoList, err := s.Store.ListMemos(ctx, &store.FindMemo{
			VisibilityList: []store.Visibility{store.Public},
		})
		if err != nil {
			return err
		}
		for _, memo := range memoList {
			urlsets = append(urlsets, fmt.Sprintf(`<url><loc>%s</loc></url>`, fmt.Sprintf("%s/m/%d", instanceURL, memo.ID)))
		}
		sitemap := fmt.Sprintf(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:news="http://www.google.com/schemas/sitemap-news/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml" xmlns:mobile="http://www.google.com/schemas/sitemap-mobile/1.0" xmlns:image="http://www.google.com/schemas/sitemap-image/1.1" xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">%s</urlset>`, strings.Join(urlsets, "\n"))
		return c.XMLBlob(http.StatusOK, []byte(sitemap))
	})
}

func generateMemoMetadata(memo *store.Memo, creator *store.User) string {
	description := ""
	if memo.Visibility == store.Private {
		description = "This memo is private."
	} else if memo.Visibility == store.Protected {
		description = "This memo is protected."
	} else {
		tokens := tokenizer.Tokenize(memo.Content)
		nodes, _ := parser.Parse(tokens)
		description = renderer.NewStringRenderer().Render(nodes)
		if len(description) == 0 {
			description = memo.Content
		}
		if len(description) > maxMetadataDescriptionLength {
			description = description[:maxMetadataDescriptionLength] + "..."
		}
	}

	metadataList := []string{
		fmt.Sprintf(`<meta name="description" content="%s" />`, template.HTMLEscapeString(description)),
		fmt.Sprintf(`<meta property="og:title" content="%s" />`, template.HTMLEscapeString(fmt.Sprintf("%s(@%s) on Memos", creator.Nickname, creator.Username))),
		fmt.Sprintf(`<meta property="og:description" content="%s" />`, template.HTMLEscapeString(description)),
		fmt.Sprintf(`<meta property="og:image" content="%s" />`, "https://www.usememos.com/logo.png"),
		`<meta property="og:type" content="website" />`,
		// Twitter related metadata.
		fmt.Sprintf(`<meta name="twitter:title" content="%s" />`, template.HTMLEscapeString(fmt.Sprintf("%s(@%s) on Memos", creator.Nickname, creator.Username))),
		fmt.Sprintf(`<meta name="twitter:description" content="%s" />`, template.HTMLEscapeString(description)),
		fmt.Sprintf(`<meta name="twitter:image" content="%s" />`, "https://www.usememos.com/logo.png"),
		`<meta name="twitter:card" content="summary" />`,
	}
	return strings.Join(metadataList, "\n")
}

func getRawIndexHTML() string {
	bytes, _ := os.ReadFile("dist/index.html")
	return string(bytes)
}
