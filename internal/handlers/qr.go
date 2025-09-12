package handlers

import (
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
    "time"

	"github.com/gin-gonic/gin"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
	"github.com/yeqown/go-qrcode/writer/standard/shapes"
)

// min4 returns the minimum of four integers.
func min4(a, b, c, d int) int {
    m := a
    if b < m { m = b }
    if c < m { m = c }
    if d < m { m = d }
    return m
}

// QRCodeHandler generates QR codes for URLs with advanced customization options
func (h *Handler) QRCodeHandler(c *gin.Context) {
    url := c.Query("url")
    if url == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "URL parameter is required"})
        return
    }

    // Parse format parameter (default to PNG)
    format := strings.ToLower(c.DefaultQuery("format", "png"))
    if format != "png" && format != "svg" {
        format = "png"
    }

	// Parse customization parameters
	colorMode := c.DefaultQuery("colorMode", "flat")
	bgColor := parseColorParam(c.Query("bg"), color.RGBA{255, 255, 255, 255}) // Default white
	cornerStyle := c.DefaultQuery("cornerStyle", "none")
	borderPattern := c.DefaultQuery("borderPattern", "simple")
	borderColorParam := c.Query("borderColor")
	// Combine corner style and border pattern
	var frame string
	if cornerStyle == "none" {
		frame = "none"
	} else if cornerStyle == "rounded" {
		frame = "rounded-" + borderPattern
	} else {
		frame = borderPattern
	}

	// Fixed 7% padding
	border := 7

    // Base frame width percent
    frameWidthPercent := 4
    // Make rounded frames start thicker before carving so final result
    // remains visually strong after the rounded inner cut.
    if strings.HasPrefix(frame, "rounded-") {
        frameWidthPercent = 6 // effective ~4% after inner carve
    }

    // Parse size parameter for different resolutions
    size := c.DefaultQuery("size", "preview") // "preview" or "download"

    // Basic request debug info
    fmt.Printf("[QR] request start: url=%q format=%s size=%s colorMode=%s qrShape=%s branding=%s\n",
        url, format, size, c.DefaultQuery("colorMode", "flat"), c.DefaultQuery("qrShape", "rectangle"), c.DefaultQuery("branding", "default"))

	// Handle color mode
	var useGradient bool
	var gradient *standard.LinearGradient
	var fgColor color.RGBA
	var gradientStartColor color.RGBA

	if colorMode == "gradient" {
		// Parse gradient colors
		startColor := parseColorParam(c.Query("gradientStart"), color.RGBA{0, 0, 0, 255})
		middleColor := parseColorParam(c.Query("gradientMiddle"), color.RGBA{128, 128, 128, 255})
		endColor := parseColorParam(c.Query("gradientEnd"), color.RGBA{255, 0, 0, 255})

		// Store start color for logo branding
		gradientStartColor = startColor

		// Create gradient with 45-degree angle
		gradient = standard.NewGradient(45, []standard.ColorStop{
			{T: 0, Color: startColor},
			{T: 0.5, Color: middleColor},
			{T: 1, Color: endColor},
		}...)
		useGradient = true
	} else {
		// Flat color mode
		fgColor = parseColorParam(c.Query("fg"), color.RGBA{0, 0, 0, 255})
		useGradient = false
	}

	// Parse border color - use foreground/gradient start as default
	var borderColor color.RGBA
	if borderColorParam != "" {
		borderColor = parseColorParam(borderColorParam, color.RGBA{0, 0, 0, 255})
	} else {
		// Default to foreground color or gradient start color
		if useGradient {
			borderColor = gradientStartColor
		} else {
			borderColor = fgColor
		}
	}

	// Parse QR shape parameter
	qrShape := c.DefaultQuery("qrShape", "rectangle")

	// Parse branding parameters
	branding := c.DefaultQuery("branding", "default")
	customDomain := c.Query("customDomain")
	centerLogo := c.DefaultQuery("centerLogo", "false")
	logoFile := c.Query("logoFile")

	// Create QR code instance with Q error correction level
	qrc, err := qrcode.NewWith(url, qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionQuart))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create QR code"})
		return
	}

    if format == "svg" {
        // Generate SVG format
		// Pass gradient colors if available
		var startColor, middleColor, endColor color.RGBA
		if useGradient {
			startColor = parseColorParam(c.Query("gradientStart"), color.RGBA{0, 0, 0, 255})
			middleColor = parseColorParam(c.Query("gradientMiddle"), color.RGBA{128, 128, 128, 255})
			endColor = parseColorParam(c.Query("gradientEnd"), color.RGBA{255, 0, 0, 255})
		}
		h.generateSVGQR(c, qrc, useGradient, gradient, fgColor, gradientStartColor, bgColor, startColor, middleColor, endColor, borderColor, border, frame, frameWidthPercent, size, qrShape, branding, customDomain, centerLogo, logoFile)
	} else {
		// Generate PNG format (default)
		// Pass gradient colors if available
		var startColor, middleColor, endColor color.RGBA
		if useGradient {
			startColor = parseColorParam(c.Query("gradientStart"), color.RGBA{0, 0, 0, 255})
			middleColor = parseColorParam(c.Query("gradientMiddle"), color.RGBA{128, 128, 128, 255})
			endColor = parseColorParam(c.Query("gradientEnd"), color.RGBA{255, 0, 0, 255})
		}
        // Add debug header for quick inspection from devtools
        c.Header("X-QR-Debug", fmt.Sprintf("format=png;size=%s;shape=%s;colorMode=%s", size, qrShape, colorMode))
        h.generatePNGQR(c, qrc, useGradient, gradient, fgColor, gradientStartColor, bgColor, startColor, middleColor, endColor, borderColor, border, frame, frameWidthPercent, size, qrShape, branding, customDomain, centerLogo, logoFile)
    }
}

// generatePNGQR generates a PNG QR code
func (h *Handler) generatePNGQR(c *gin.Context, qrc *qrcode.QRCode, useGradient bool, gradient *standard.LinearGradient, fgColor, gradientStartColor, bgColor color.RGBA, gradientStart, gradientMiddle, gradientEnd color.RGBA, borderColor color.RGBA, border int, frame string, frameWidthPercent int, size string, qrShape string, branding string, customDomain string, centerLogo string, logoFile string) {
	// Create unique temporary file for PNG output
	tmpFile := filepath.Join(os.TempDir(), generateUniqueFilename("qr", ".png"))

	// Set module size based on requested size
	var moduleSize uint8
	if size == "download" {
		// For 2000x2000 target: 2000 / 21 modules ≈ 95 pixels per module
		// Let's use a large but safe value
		moduleSize = 120 // Should give us ~2520x2520 for typical QR
	} else {
		moduleSize = 16 // Slightly larger preview (336px target)
	}

	var writerOptions []standard.ImageOption
	baseOptions := []standard.ImageOption{
		standard.WithQRWidth(moduleSize),
		standard.WithBorderWidth(0), // Generate clean QR without borders
	}

	// Add center logo if requested
	if centerLogo == "true" {
		var logoPath string
		if logoFile != "" {
			// Use uploaded logo file
			logoPath = filepath.Join("uploads", logoFile)
		} else {
			// Use default uploaded logo
			logoPath = "uploads/temp_logo.png"
		}

		if _, err := os.Stat(logoPath); err == nil {
			baseOptions = append(baseOptions, standard.WithLogoImageFilePNG(logoPath))
		}
	}

	// Handle background color - transparent or solid
	if bgColor.A == 0 {
		// Transparent background
		fmt.Printf("DEBUG: Using transparent background for PNG\n")
		baseOptions = append(baseOptions, standard.WithBgTransparent())
		baseOptions = append(baseOptions, standard.WithBuiltinImageEncoder(standard.PNG_FORMAT))
	} else {
		// Solid background color
		fmt.Printf("DEBUG: Using solid background color: %+v\n", bgColor)
		baseOptions = append(baseOptions, standard.WithBgColor(bgColor))
	}

	// Add shape option based on qrShape parameter
	switch qrShape {
	case "circle":
		baseOptions = append(baseOptions, standard.WithCircleShape())
	case "liquid":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.LiquidBlock()}))
	case "chain":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.ChainBlock()}))
	case "hstripe":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.HStripeBlock(0.85)}))
	case "vstripe":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.VStripeBlock(0.85)}))
	default:
		// rectangle - default shape, no additional options needed
	}

	if useGradient {
		writerOptions = append(baseOptions, standard.WithFgGradient(gradient))
	} else {
		writerOptions = append(baseOptions, standard.WithFgColor(fgColor))
	}

	writer, err := standard.New(tmpFile, writerOptions...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create QR writer"})
		return
	}

	// Write QR code to file
	if err := qrc.Save(writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate QR code image: %v", err)})
		return
	}

	// Clean up anti-aliasing artifacts (white border pixels) for transparent background
	if bgColor.A == 0 {
		if err := h.cleanupAntiAliasing(tmpFile, fgColor); err != nil {
			fmt.Printf("Warning: Failed to cleanup anti-aliasing: %v\n", err)
		}
	}

	// Ensure the writer is properly closed and synced
	writer.Close()

	// Debug: Check actual generated size
	if file, err := os.Open(tmpFile); err == nil {
		if img, _, err := image.DecodeConfig(file); err == nil {
			fmt.Printf("Generated QR size: %dx%d (requested moduleSize: %d, size: %s)\n",
				img.Width, img.Height, moduleSize, size)
		}
		file.Close()
	}

	// Force file system sync
	if file, err := os.OpenFile(tmpFile, os.O_RDWR, 0); err == nil {
		file.Sync()
		file.Close()
	}

    // For download size, ensure we reach target dimensions
    if size == "download" {
        if err := h.ensureMinimumQRSize(tmpFile, 2000, bgColor); err != nil {
            fmt.Printf("Warning: Could not scale QR to target size: %v\n", err)
        }
    }

    // Store original QR size before any modifications
    originalSize := 0
    if file, err := os.Open(tmpFile); err == nil {
        if img, _, err := image.DecodeConfig(file); err == nil {
            originalSize = img.Width
        }
        file.Close()
    }

    // For preview: scale the base QR to a size that will produce the exact
    // requested previewSize AFTER padding and frame, so we don't scale the
    // decorative frame later (which causes aliasing/dotting artifacts).
    didPreviewPreScale := false
    if size == "preview" {
        if ps := c.Query("previewSize"); ps != "" {
            if target, err := strconv.Atoi(ps); err == nil && target > 0 && originalSize > 0 {
                // final = base + 2*(padding + frame) where padding = originalSize*border/100
                // and frame = originalSize*frameWidthPercent/100
                multiplier := 1.0 + 2.0*((float64(border)+float64(frameWidthPercent))/100.0)
                desiredBase := int(math.Round(float64(target) / multiplier))
                if desiredBase > 0 && desiredBase != originalSize {
                    if err := h.ensureExactQRSize(tmpFile, desiredBase); err == nil {
                        // update originalSize to the new base size
                        if file, err := os.Open(tmpFile); err == nil {
                            if img, _, err := image.DecodeConfig(file); err == nil {
                                originalSize = img.Width
                            }
                            file.Close()
                        }
                        didPreviewPreScale = true
                    }
                }
            }
        }
    }

	// Step 1: Add logo to original QR (before padding/frame)
	brandingColor := fgColor
	if useGradient {
		brandingColor = gradientStartColor // Use gradient start color for logo
	}
	if err := h.addLogoToOriginalQR(tmpFile, brandingColor, bgColor, originalSize, useGradient, gradientStart, gradientMiddle, gradientEnd, qrShape, branding, customDomain); err != nil {
		fmt.Printf("Warning: Could not add logo to QR: %v\n", err)
	}

    // Step 2: Add padding around QR+logo - use transparent padding for transparent QRs
	if border > 0 {
		paddingBgColor := bgColor
		if bgColor.A == 0 {
			paddingBgColor = color.RGBA{0, 0, 0, 0} // Ensure truly transparent
		}
		if err := h.addAbsolutePaddingToQRFile(tmpFile, border, originalSize, paddingBgColor); err != nil {
			fmt.Printf("Warning: Could not add padding to QR: %v\n", err)
		}
	}

    // Step 3: Add decorative frame around everything - with appropriate background
	if frame != "none" {
		framePixels := (originalSize * frameWidthPercent) / 100
		frameBgColor := color.RGBA{255, 255, 255, 255} // Default white
		if bgColor.A == 0 {
			frameBgColor = color.RGBA{0, 0, 0, 0} // Transparent for transparent QRs
		}
		if err := h.addFrameToQRFile(tmpFile, frame, framePixels, frameBgColor, borderColor, useGradient, gradientStart, gradientMiddle, gradientEnd); err != nil {
			fmt.Printf("Warning: Could not add frame to QR: %v\n", err)
		}
	}

    // If preview and we did not pre-scale, fall back to final scaling as before
    if size == "preview" && !didPreviewPreScale {
        if ps := c.Query("previewSize"); ps != "" {
            if target, err := strconv.Atoi(ps); err == nil && target > 0 {
                if err := h.ensureExactQRSize(tmpFile, target); err != nil {
                    fmt.Printf("Warning: Could not scale QR to preview size: %v\n", err)
                }
            }
        }
    }

    // Verify file exists and has content
    fileInfo, err := os.Stat(tmpFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Generated QR file not found: %v", err)})
		return
	}
	if fileInfo.Size() == 0 {
		os.Remove(tmpFile) // Clean up empty file
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Generated QR file is empty"})
		return
	}

    // Read the file and send it
    file, err := os.Open(tmpFile)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to read QR code file: %v", err)})
        return
    }
    defer file.Close()
    defer os.Remove(tmpFile) // Clean up temp file

    // Set headers for PNG image
    c.Header("Content-Type", "image/png")
    c.Header("Cache-Control", "public, max-age=3600") // Cache for 1 hour

    // Copy file content to response
    n, err := io.Copy(c.Writer, file)
    fmt.Printf("[QR] sent PNG bytes=%d size=%s shape=%s\n", n, size, qrShape)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send QR code"})
        return
    }
}

// generateSVGQR generates a true vector SVG QR code
func (h *Handler) generateSVGQR(c *gin.Context, qrc *qrcode.QRCode, useGradient bool, gradient *standard.LinearGradient, fgColor, gradientStartColor, bgColor color.RGBA, gradientStart, gradientMiddle, gradientEnd color.RGBA, borderColor color.RGBA, border int, frame string, frameWidthPercent int, size string, qrShape string, branding string, customDomain string, centerLogo string, logoFile string) {
	// Generate true vector SVG from QR matrix data
	if err := h.generateVectorSVG(c, qrc, useGradient, fgColor, gradientStartColor, bgColor, gradientStart, gradientMiddle, gradientEnd, borderColor, border, frame, frameWidthPercent, size, qrShape, branding, customDomain, centerLogo, logoFile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate vector SVG: %v", err)})
		return
	}
}

// generateVectorSVG creates a true vector SVG QR code from matrix data
func (h *Handler) generateVectorSVG(c *gin.Context, qrc *qrcode.QRCode, useGradient bool, fgColor, gradientStartColor, bgColor color.RGBA, gradientStart, gradientMiddle, gradientEnd color.RGBA, borderColor color.RGBA, border int, frame string, frameWidthPercent int, size string, qrShape string, branding string, customDomain string, centerLogo string, logoFile string) error {
	// Get QR matrix dimensions and bitmap data
	dimension := qrc.Dimension()
	if dimension <= 0 {
		return fmt.Errorf("invalid QR matrix dimension")
	}

	// Access QR matrix bitmap through a different approach
	// We'll need to create a temporary bitmap by iterating through the matrix

	// Calculate module size for different target sizes
	var moduleSize int
	var targetSize int
	if size == "download" {
		targetSize = 2000
		moduleSize = targetSize / dimension
	} else {
		targetSize = 400 // Preview size
		moduleSize = targetSize / dimension
	}

	// Calculate total SVG size including padding and frame
	paddingPixels := (targetSize * border) / 100
	framePixels := 0
	if frame != "none" {
		framePixels = (targetSize * frameWidthPercent) / 100
	}

	totalSize := targetSize + (paddingPixels * 2) + (framePixels * 2)
	qrOffset := framePixels + paddingPixels

	// Start building SVG content
	svgBuilder := strings.Builder{}
	svgBuilder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	svgBuilder.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`,
		totalSize, totalSize, totalSize, totalSize))

	// Add definitions for gradients if needed
	if useGradient {
		svgBuilder.WriteString(`<defs>`)
		svgBuilder.WriteString(`<linearGradient id="qrGradient" x1="0%" y1="0%" x2="100%" y2="100%">`)
		svgBuilder.WriteString(fmt.Sprintf(`<stop offset="0%%" stop-color="rgb(%d,%d,%d)"/>`,
			gradientStart.R, gradientStart.G, gradientStart.B))
		svgBuilder.WriteString(fmt.Sprintf(`<stop offset="50%%" stop-color="rgb(%d,%d,%d)"/>`,
			gradientMiddle.R, gradientMiddle.G, gradientMiddle.B))
		svgBuilder.WriteString(fmt.Sprintf(`<stop offset="100%%" stop-color="rgb(%d,%d,%d)"/>`,
			gradientEnd.R, gradientEnd.G, gradientEnd.B))
		svgBuilder.WriteString(`</linearGradient>`)
		svgBuilder.WriteString(`</defs>`)
	}

	// Add background
	if bgColor.A > 0 {
		svgBuilder.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="rgb(%d,%d,%d)"/>`,
			totalSize, totalSize, bgColor.R, bgColor.G, bgColor.B))
	}

	// Add frame if requested
	if frame != "none" {
		frameFillColor := fmt.Sprintf("rgb(%d,%d,%d)", borderColor.R, borderColor.G, borderColor.B)
		if useGradient {
			frameFillColor = "url(#qrGradient)"
		}

		// Create simple frame as a border (4 rectangles around the edges)
		// Top border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="0" y="0" width="%d" height="%d" fill="%s"/>`,
			totalSize, framePixels, frameFillColor))
		// Bottom border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="0" y="%d" width="%d" height="%d" fill="%s"/>`,
			totalSize-framePixels, totalSize, framePixels, frameFillColor))
		// Left border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="0" y="%d" width="%d" height="%d" fill="%s"/>`,
			framePixels, framePixels, totalSize-(2*framePixels), frameFillColor))
		// Right border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
			totalSize-framePixels, framePixels, framePixels, totalSize-(2*framePixels), frameFillColor))
	}

	// Generate QR modules as SVG paths
	fillColor := fmt.Sprintf("rgb(%d,%d,%d)", fgColor.R, fgColor.G, fgColor.B)
	if useGradient {
		fillColor = "url(#qrGradient)"
	}

	// Create a bitmap by examining a temporary PNG first
	// Since we can't directly access the matrix, we'll generate a minimal PNG first
	tmpFile := filepath.Join(os.TempDir(), generateUniqueFilename("qr_matrix", ".png"))
	defer os.Remove(tmpFile)

	// Create minimal writer for matrix extraction
	writer, err := standard.New(tmpFile, standard.WithQRWidth(1), standard.WithBorderWidth(0), standard.WithBgColor(color.RGBA{255, 255, 255, 255}), standard.WithFgColor(color.RGBA{0, 0, 0, 255}))
	if err != nil {
		return fmt.Errorf("failed to create QR writer for matrix extraction: %v", err)
	}

	if err := qrc.Save(writer); err != nil {
		return fmt.Errorf("failed to generate QR for matrix extraction: %v", err)
	}
	writer.Close()

	// Read the generated PNG to extract the matrix
	file, err := os.Open(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to open matrix file: %v", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("failed to decode matrix image: %v", err)
	}

	bounds := img.Bounds()
	actualDimension := bounds.Dx() // Should match our calculated dimension

	// Iterate through image and create rectangles for dark pixels
	for y := 0; y < actualDimension; y++ {
		for x := 0; x < actualDimension; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r < 32768 { // Dark pixel (closer to black than white)
				// Scale from 1-pixel module to target moduleSize
				moduleX := qrOffset + (x * moduleSize)
				moduleY := qrOffset + (y * moduleSize)

				// Apply shape based on qrShape parameter
				switch qrShape {
				case "circle":
					radius := moduleSize / 2
					centerX := moduleX + radius
					centerY := moduleY + radius
					svgBuilder.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="%s"/>`,
						centerX, centerY, radius, fillColor))
				default: // rectangle
					svgBuilder.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
						moduleX, moduleY, moduleSize, moduleSize, fillColor))
				}
			}
		}
	}

	// Add center logo if requested
	if centerLogo == "true" {
		centerX := totalSize / 2
		centerY := totalSize / 2
		logoSize := targetSize / 4 // 25% of QR size

		// Create a white background circle for the logo
		svgBuilder.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="white"/>`,
			centerX, centerY, logoSize/2+5))

		// Add logo placeholder (this would need actual logo SVG content)
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="gray" opacity="0.3"/>`,
			centerX-logoSize/2, centerY-logoSize/2, logoSize, logoSize))
	}

	// Add branding logo
	if branding != "none" {
		if err := h.addSVGBrandingLogo(&svgBuilder, branding, customDomain, fgColor, useGradient, fillColor, qrOffset, targetSize, totalSize); err != nil {
			fmt.Printf("Warning: Could not add SVG branding logo: %v\n", err)
		}
	}

	// Close SVG
	svgBuilder.WriteString(`</svg>`)

	// Return SVG content
	c.Header("Content-Type", "image/svg+xml")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "image/svg+xml", []byte(svgBuilder.String()))

	return nil
}

// addSVGBrandingLogo adds the branding logo as SVG elements
func (h *Handler) addSVGBrandingLogo(svgBuilder *strings.Builder, branding string, customDomain string, fgColor color.RGBA, useGradient bool, fillColor string, qrOffset int, targetSize int, totalSize int) error {
	if branding == "none" {
		return nil
	}

	// Calculate logo positioning (bottom-right within QR area)
	logoSize := targetSize / 3   // Same size calculation as PNG version
	marginX := targetSize/45 + 1 // Same margin as PNG version
	marginY := 8

	logoX := qrOffset + targetSize - logoSize - marginX
	logoY := qrOffset + targetSize - logoSize - marginY

	if customDomain != "" && branding == "custom" {
		// Add custom domain text
		svgBuilder.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial, sans-serif" font-size="%d" font-weight="bold" fill="%s">%s</text>`,
			logoX, logoY+logoSize/2, logoSize/8, fillColor, customDomain))
	} else if branding == "default" {
		// Add shortn.link branding - for now, just text (would need actual SVG logo content)
		fontSize := logoSize / 10
		svgBuilder.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial, sans-serif" font-size="%d" font-weight="bold" fill="%s">shortn.link</text>`,
			logoX, logoY+logoSize/2, fontSize, fillColor))
	}

	return nil
}

// Helper function to parse integer parameters
func parseIntParam(param string, defaultValue int) int {
	if param == "" {
		return defaultValue
	}
	if value, err := strconv.Atoi(param); err == nil {
		return value
	}
	return defaultValue
}

// Helper function to parse hex color parameters
func parseColorParam(param string, defaultColor color.RGBA) color.RGBA {
	if param == "" {
		return defaultColor
	}

	// Handle transparent background
	if strings.ToLower(param) == "transparent" {
		return color.RGBA{0, 0, 0, 0} // Fully transparent
	}

	// Remove # if present
	param = strings.TrimPrefix(param, "#")

	// Ensure it's 6 characters
	if len(param) != 6 {
		return defaultColor
	}

	// Parse hex values
	r, err1 := strconv.ParseUint(param[0:2], 16, 8)
	g, err2 := strconv.ParseUint(param[2:4], 16, 8)
	b, err3 := strconv.ParseUint(param[4:6], 16, 8)

	if err1 != nil || err2 != nil || err3 != nil {
		return defaultColor
	}

	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

// Helper function to generate unique temporary filenames
func generateUniqueFilename(prefix, extension string) string {
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	return fmt.Sprintf("%s_%d_%x%s", prefix, timestamp, randomBytes, extension)
}

// addBrandingToReservedSpace adds branding logo to the reserved right border space
func (h *Handler) addBrandingToReservedSpace(filename string, fgColor, bgColor color.RGBA, reservedSpace int) error {
    // PNG branding removed; no-op.
    return nil
}

// addBrandingOverlay adds branding logo to bottom-right corner, accounting for user borders
func (h *Handler) addBrandingOverlay(filename string, fgColor, bgColor color.RGBA, userBorder int, frameType string) error {
	// Open and decode the existing QR PNG (with user borders already applied)
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	// Create a new RGBA image from the QR code
	bounds := qrImg.Bounds()
	brandedImg := image.NewRGBA(bounds)
	draw.Draw(brandedImg, bounds, qrImg, image.Point{}, draw.Src)

	// Calculate target logo size based on final image size, preserving aspect ratio
	finalImageSize := bounds.Dx()
	maxLogoWidth := int(float64(finalImageSize) * 0.35)  // Max 35% of final image width (increased from 25%)
	maxLogoHeight := int(float64(finalImageSize) * 0.20) // Max 20% of final image height (increased from 15%)

	// Load logo (will be SVG at proper aspect ratio, or PNG at original size)
	logoImg, err := h.loadLogoImage(maxLogoWidth, maxLogoHeight)
	if err != nil {
		return fmt.Errorf("failed to load branding logo: %v", err)
	}

	// Get logo dimensions
	logoBounds := logoImg.Bounds()
	logoWidth := logoBounds.Dx()
	logoHeight := logoBounds.Dy()

	// Calculate final size preserving aspect ratio
	var scaledWidth, scaledHeight int

	// If this looks like it came from SVG (within our max constraints), use as-is
	if logoWidth <= maxLogoWidth && logoHeight <= maxLogoHeight {
		scaledWidth = logoWidth
		scaledHeight = logoHeight
	} else {
		// PNG case: scale preserving aspect ratio
		aspectRatio := float64(logoWidth) / float64(logoHeight)
		scaledWidth = maxLogoWidth
		scaledHeight = int(float64(scaledWidth) / aspectRatio)

		// If height exceeds limit, scale based on height
		if scaledHeight > maxLogoHeight {
			scaledHeight = maxLogoHeight
			scaledWidth = int(float64(scaledHeight) * aspectRatio)
		}
	}

	// Frame thickness is handled in the positioning calculation below

	// Calculate original QR area within the padded/framed image
	// We need to position relative to the original QR, not the final image bounds

	// Calculate how much padding was added (percentage of original QR size)
	totalImageSize := bounds.Dx() // Assuming square

	// Estimate original QR size by working backwards from padding
	// If userBorder = 10%, then paddedSize = originalSize * 1.2 (20% total padding)
	paddingFactor := 1.0 + (float64(userBorder) * 2.0 / 100.0) // *2 because padding on both sides
	estimatedOriginalSize := int(float64(totalImageSize) / paddingFactor)
	paddingPixels := (totalImageSize - estimatedOriginalSize) / 2

	// Account for frame thickness
	if frameType != "none" {
		frameThickness := 20
		if frameType == "thick" {
			frameThickness = 40
		}
		paddingPixels -= frameThickness
		estimatedOriginalSize += frameThickness * 2
	}

	// Calculate original QR boundaries within the final image
	originalQRLeft := paddingPixels
	originalQRTop := paddingPixels
	originalQRRight := originalQRLeft + estimatedOriginalSize
	originalQRBottom := originalQRTop + estimatedOriginalSize

	// Position logo relative to original QR area, not final image
	marginX := 25      // Significantly increased horizontal margin from QR edge
	marginY := 30      // Significantly increased vertical margin for more top padding
	padding := 8       // Minimal background clearing area around logo
	bottomPadding := 8 // Extra padding at bottom to cover QR pattern

	x := originalQRRight - scaledWidth - marginX
	y := originalQRBottom - scaledHeight - marginY

	// Create padded rectangle for clearing area behind logo (extra padding at bottom)
	paddedRect := image.Rect(x-padding, y-padding, x+scaledWidth+padding, y+scaledHeight+bottomPadding)

	// Clear the padded logo area (use background color)
	for py := paddedRect.Min.Y; py < paddedRect.Max.Y; py++ {
		for px := paddedRect.Min.X; px < paddedRect.Max.X; px++ {
			if px >= 0 && py >= 0 && px < bounds.Max.X && py < bounds.Max.Y {
				brandedImg.Set(px, py, bgColor)
			}
		}
	}

	// Draw logo with appropriate method (direct copy for SVG, scaled for PNG)
	if scaledWidth == logoBounds.Dx() && scaledHeight == logoBounds.Dy() {
		// SVG case: direct copy (already rendered at correct size)
		for py := 0; py < scaledHeight; py++ {
			for px := 0; px < scaledWidth; px++ {
				logoColor := logoImg.At(logoBounds.Min.X+px, logoBounds.Min.Y+py)
				if _, _, _, a := logoColor.RGBA(); a > 32768 { // If not transparent
					if x+px >= 0 && y+py >= 0 && x+px < bounds.Max.X && y+py < bounds.Max.Y {
						brandedImg.Set(x+px, y+py, fgColor)
					}
				}
			}
		}
	} else {
		// PNG case: scale with bilinear interpolation
		scale := float64(scaledWidth) / float64(logoBounds.Dx())
		for py := 0; py < scaledHeight; py++ {
			for px := 0; px < scaledWidth; px++ {
				// Calculate precise original coordinates for bilinear interpolation
				origXf := float64(px) / scale
				origYf := float64(py) / scale

				// Get the four nearest pixels for bilinear interpolation
				x1 := int(origXf)
				y1 := int(origYf)
				x2 := x1 + 1
				y2 := y1 + 1

				// Ensure coordinates are within bounds
				if x1 < 0 {
					x1 = 0
				}
				if y1 < 0 {
					y1 = 0
				}
				if x2 >= logoBounds.Dx() {
					x2 = logoBounds.Dx() - 1
				}
				if y2 >= logoBounds.Dy() {
					y2 = logoBounds.Dy() - 1
				}

				// Sample the four corner pixels
				c1 := logoImg.At(logoBounds.Min.X+x1, logoBounds.Min.Y+y1)
				c2 := logoImg.At(logoBounds.Min.X+x2, logoBounds.Min.Y+y1)
				c3 := logoImg.At(logoBounds.Min.X+x1, logoBounds.Min.Y+y2)
				c4 := logoImg.At(logoBounds.Min.X+x2, logoBounds.Min.Y+y2)

				// Get alpha values for interpolation
				_, _, _, a1 := c1.RGBA()
				_, _, _, a2 := c2.RGBA()
				_, _, _, a3 := c3.RGBA()
				_, _, _, a4 := c4.RGBA()

				// Calculate interpolation weights
				wx := origXf - float64(x1)
				wy := origYf - float64(y1)

				// Bilinear interpolation of alpha values
				alpha := (1-wx)*(1-wy)*float64(a1) + wx*(1-wy)*float64(a2) +
					(1-wx)*wy*float64(a3) + wx*wy*float64(a4)

				// If interpolated alpha indicates non-transparent pixel
				if alpha > 32768 { // Threshold for non-transparent
					if x+px >= 0 && y+py >= 0 && x+px < bounds.Max.X && y+py < bounds.Max.Y {
						brandedImg.Set(x+px, y+py, fgColor)
					}
				}
			}
		}
	}

	// Save the branded image back to the file
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, brandedImg); err != nil {
		return fmt.Errorf("failed to encode branded PNG: %v", err)
	}

	return nil
}

// loadLogoImage tries to load SVG logo first, falls back to PNG
// maxWidth and maxHeight are constraints - actual size will preserve aspect ratio
func (h *Handler) loadLogoImage(maxWidth, maxHeight int) (image.Image, error) {
    // Only support SVG logo; if not present, indicate no logo available.
    svgPath := "web/static/shortnlink-h-logo.svg"
    if _, err := os.Stat(svgPath); err == nil {
        return h.renderSVGLogoWithAspect(svgPath, maxWidth, maxHeight)
    }
    // No SVG logo; skip silently by returning os.ErrNotExist
    return nil, os.ErrNotExist
}

// renderSVGLogoWithAspect renders SVG preserving aspect ratio within max constraints
func (h *Handler) renderSVGLogoWithAspect(svgPath string, maxWidth, maxHeight int) (image.Image, error) {
	// Read SVG file
	svgFile, err := os.Open(svgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open SVG: %v", err)
	}
	defer svgFile.Close()

	// Parse SVG
	svgIcon, parseErr := oksvg.ReadIconStream(svgFile)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse SVG: %v", parseErr)
	}

	// Get SVG viewbox to determine aspect ratio
	viewBox := svgIcon.ViewBox
	originalWidth := viewBox.W
	originalHeight := viewBox.H

	if originalWidth <= 0 || originalHeight <= 0 {
		originalWidth = 100 // fallback
		originalHeight = 30 // reasonable aspect for text logo
	}

	// Calculate size preserving aspect ratio
	aspectRatio := originalWidth / originalHeight
	targetWidth := maxWidth
	targetHeight := int(float64(targetWidth) / aspectRatio)

	// If height exceeds limit, scale down based on height
	if targetHeight > maxHeight {
		targetHeight = maxHeight
		targetWidth = int(float64(targetHeight) * aspectRatio)
	}

	// Set target size
	svgIcon.SetTarget(0, 0, float64(targetWidth), float64(targetHeight))

	// Create image to render to
	img := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	// Create scanner/rasterizer
	scanner := rasterx.NewScannerGV(targetWidth, targetHeight, img, img.Bounds())

	// Render SVG to image
	raster := rasterx.NewDasher(targetWidth, targetHeight, scanner)
	svgIcon.Draw(raster, 1.0) // 1.0 = full opacity

	return img, nil
}

// generateCustomDomainSVG creates an SVG with custom domain text using Mukta Malar font
func (h *Handler) generateCustomDomainSVG(domain string, maxWidth, maxHeight int, textColor color.RGBA) (image.Image, error) {
	// Calculate appropriate font size
	fontSize := maxHeight * 3 / 4
	if fontSize < 12 {
		fontSize = 12
	}
	if fontSize > 24 {
		fontSize = 24
	}

	// Estimate and adjust width
	estimatedWidth := int(float64(fontSize) * 0.6 * float64(len(domain)))
	if estimatedWidth > maxWidth {
		fontSize = int(float64(maxWidth) / (0.6 * float64(len(domain))))
		if fontSize < 8 {
			fontSize = 8
		}
	}

	textWidth := int(float64(fontSize) * 0.6 * float64(len(domain)))
	textHeight := fontSize + 4

	// Create SVG with inline font attributes
	svgContent := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d"><text x="%d" y="%d" font-family="Arial,sans-serif" font-size="%d" font-weight="600" fill="rgb(%d,%d,%d)" text-anchor="middle" dominant-baseline="middle">%s</text></svg>`,
		textWidth, textHeight, textWidth, textHeight,
		textWidth/2, textHeight/2, fontSize,
		textColor.R, textColor.G, textColor.B, domain)

	domainIcon, parseErr := oksvg.ReadIconStream(strings.NewReader(svgContent))
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse custom domain SVG: %v", parseErr)
	}

	domainIcon.SetTarget(0, 0, float64(textWidth), float64(textHeight))
	img := image.NewRGBA(image.Rect(0, 0, textWidth, textHeight))
	scanner := rasterx.NewScannerGV(textWidth, textHeight, img, img.Bounds())
	raster := rasterx.NewDasher(textWidth, textHeight, scanner)
	domainIcon.Draw(raster, 1.0)

	return img, nil
}

// ensureMinimumQRSize scales up QR code if it's smaller than target size
func (h *Handler) ensureMinimumQRSize(filename string, minSize int, bgColor color.RGBA) error {
	// Open and check current size
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	currentSize := bounds.Dx() // Assuming square QR

	fmt.Printf("Current QR size: %dx%d, target: %dx%d\n", currentSize, bounds.Dy(), minSize, minSize)

	// If already large enough, no scaling needed
	if currentSize >= minSize {
		return nil
	}

	// Calculate scale factor (use nearest neighbor for QR codes to keep sharp edges)
	scaleFactor := float64(minSize) / float64(currentSize)
	newSize := int(float64(currentSize) * scaleFactor)

	fmt.Printf("Scaling QR by factor %.2f to %dx%d\n", scaleFactor, newSize, newSize)

	// Create new larger image
	scaledImg := image.NewRGBA(image.Rect(0, 0, newSize, newSize))

	// Scale using nearest neighbor (preserves sharp QR edges)
	for y := 0; y < newSize; y++ {
		for x := 0; x < newSize; x++ {
			// Map back to original coordinates
			origX := int(float64(x) / scaleFactor)
			origY := int(float64(y) / scaleFactor)

			// Ensure we don't go out of bounds
			if origX >= currentSize {
				origX = currentSize - 1
			}
			if origY >= bounds.Dy() {
				origY = bounds.Dy() - 1
			}

			// Copy pixel
			color := qrImg.At(bounds.Min.X+origX, bounds.Min.Y+origY)
			scaledImg.Set(x, y, color)
		}
	}

	// Save the scaled image back to file
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create scaled output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, scaledImg); err != nil {
		return fmt.Errorf("failed to encode scaled PNG: %v", err)
	}

	return nil
}

// ensureExactQRSize scales the QR code to exactly targetSize x targetSize using nearest neighbor.
func (h *Handler) ensureExactQRSize(filename string, targetSize int) error {
    // Open and decode current image
    file, err := os.Open(filename)
    if err != nil {
        return fmt.Errorf("failed to open QR file: %v", err)
    }
    img, _, err := image.Decode(file)
    file.Close()
    if err != nil {
        return fmt.Errorf("failed to decode QR image: %v", err)
    }

    bounds := img.Bounds()
    currentW := bounds.Dx()
    if currentW == 0 || targetSize <= 0 {
        return nil
    }

    scale := float64(targetSize) / float64(currentW)
    // Create destination image
    dst := image.NewRGBA(image.Rect(0, 0, targetSize, targetSize))
    for y := 0; y < targetSize; y++ {
        for x := 0; x < targetSize; x++ {
            ox := int(float64(x) / scale)
            oy := int(float64(y) / scale)
            if ox >= bounds.Dx() {
                ox = bounds.Dx() - 1
            }
            if oy >= bounds.Dy() {
                oy = bounds.Dy() - 1
            }
            dst.Set(x, y, img.At(bounds.Min.X+ox, bounds.Min.Y+oy))
        }
    }

    out, err := os.Create(filename)
    if err != nil {
        return fmt.Errorf("failed to create scaled output file: %v", err)
    }
    defer out.Close()
    if err := png.Encode(out, dst); err != nil {
        return fmt.Errorf("failed to encode scaled PNG: %v", err)
    }
    return nil
}

// addBrandingToQRFile adds "shortn.link" text to bottom-right of existing QR PNG file (legacy function)
func (h *Handler) addBrandingToQRFile(filename string, fgColor, bgColor color.RGBA, borderModules, moduleSize int) error {
    // PNG branding removed; no-op.
    return nil
}

// addPaddingToQRFile adds padding around the QR code
func (h *Handler) addPaddingToQRFile(filename string, borderModules, moduleSize int, bgColor color.RGBA) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	// Use percentage of QR size instead of modules * moduleSize
	paddingPixels := (bounds.Dx() * borderModules) / 100

	// Create new image with padding
	newWidth := bounds.Dx() + paddingPixels*2
	newHeight := bounds.Dy() + paddingPixels*2
	paddedImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Fill with background color
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			paddedImg.Set(x, y, bgColor)
		}
	}

	// Draw original QR code in center
	draw.Draw(paddedImg, image.Rect(paddingPixels, paddingPixels, paddingPixels+bounds.Dx(), paddingPixels+bounds.Dy()), qrImg, bounds.Min, draw.Src)

	// Save the padded image
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create padded output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, paddedImg); err != nil {
		return fmt.Errorf("failed to encode padded PNG: %v", err)
	}

	return nil
}

// addAbsolutePaddingToQRFile adds consistent padding regardless of QR resolution
func (h *Handler) addAbsolutePaddingToQRFile(filename string, borderPercent, originalSize int, bgColor color.RGBA) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	// Calculate padding based on percentage of original QR size for consistency
	paddingPixels := (originalSize * borderPercent) / 100
	// Scale padding proportionally if QR was resized
	if bounds.Dx() != originalSize {
		scaleFactor := float64(bounds.Dx()) / float64(originalSize)
		paddingPixels = int(float64(paddingPixels) * scaleFactor)
	}

	// Create new image with padding
	newWidth := bounds.Dx() + paddingPixels*2
	newHeight := bounds.Dy() + paddingPixels*2
	paddedImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Fill with background color only if not transparent
	if bgColor.A != 0 {
		for y := 0; y < newHeight; y++ {
			for x := 0; x < newWidth; x++ {
				paddedImg.Set(x, y, bgColor)
			}
		}
	}
	// If transparent, RGBA image starts with transparent pixels by default

	// Draw original QR code in center
	draw.Draw(paddedImg, image.Rect(paddingPixels, paddingPixels, paddingPixels+bounds.Dx(), paddingPixels+bounds.Dy()), qrImg, bounds.Min, draw.Src)

	// Save the padded image
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create padded output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, paddedImg); err != nil {
		return fmt.Errorf("failed to encode padded PNG: %v", err)
	}

	return nil
}

// addFrameToQRFile adds a decorative frame around the QR code
func (h *Handler) addFrameToQRFile(filename, frameType string, frameWidth int, bgColor, frameColor color.RGBA, useGradient bool, gradientStart, gradientMiddle, gradientEnd color.RGBA) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	// Use provided frameWidth parameter

	// Create new image with frame
	newWidth := bounds.Dx() + frameWidth*2
	newHeight := bounds.Dy() + frameWidth*2
	framedImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Fill with background color only if not transparent
	if bgColor.A != 0 {
		for y := 0; y < newHeight; y++ {
			for x := 0; x < newWidth; x++ {
				framedImg.Set(x, y, bgColor)
			}
		}
	}
	// If transparent, RGBA image starts with transparent pixels by default

    // Helper function to calculate gradient color at position (45-degree, bottom-left to top-right)
    calculateFrameColor := func(x, y int) color.RGBA {
		if !useGradient {
			return frameColor
		}

		// Calculate gradient position for 45-degree angle (bottom-left to top-right)
		// Normalize coordinates to 0-1 range
		normalizedX := float64(x) / float64(newWidth)
		normalizedY := float64(y) / float64(newHeight)

		// For 45-degree gradient from bottom-left to top-right:
		// t = 0 at bottom-left, t = 1 at top-right
		t := (normalizedX + (1.0 - normalizedY)) / 2.0

		// Clamp t to valid range
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}

		// Interpolate between the 3 gradient colors
		if t <= 0.5 {
			// Interpolate between start (t=0) and middle (t=0.5)
			localT := t * 2.0
			return h.lerpColor(gradientStart, gradientMiddle, localT)
		} else {
			// Interpolate between middle (t=0.5) and end (t=1.0)
			localT := (t - 0.5) * 2.0
			return h.lerpColor(gradientMiddle, gradientEnd, localT)
		}
	}

    // Helper: rounded rectangle hit test used for rounded gap shaping
    insideRoundedRect := func(x, y, left, top, right, bottom, r int) bool {
        if left > right || top > bottom {
            return false
        }
        if r <= 0 {
            return x >= left && x <= right && y >= top && y <= bottom
        }
        // Straight bands
        if x >= left+r && x <= right-r && y >= top && y <= bottom {
            return true
        }
        if y >= top+r && y <= bottom-r && x >= left && x <= right {
            return true
        }
        // Corner circles
        dx, dy := x-(left+r), y-(top+r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        dx, dy = x-(right-r), y-(top+r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        dx, dy = x-(left+r), y-(bottom-r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        dx, dy = x-(right-r), y-(bottom-r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        return false
    }

    // Draw frame border based on type
    for y := 0; y < newHeight; y++ {
        for x := 0; x < newWidth; x++ {
			isFrameArea := x < frameWidth || x >= newWidth-frameWidth || y < frameWidth || y >= newHeight-frameWidth
			if !isFrameArea {
				continue
			}

			// Extract base pattern from frame type
			basePattern := frameType
			if strings.HasPrefix(frameType, "rounded-") {
				basePattern = strings.TrimPrefix(frameType, "rounded-")
			}

			switch basePattern {
			case "irregular":
				// Irregular dashed pattern - random dash lengths
				cornerSize := frameWidth

				// Draw solid corners
				if (x < cornerSize && y < cornerSize) ||
					(x >= newWidth-cornerSize && y < cornerSize) ||
					(x < cornerSize && y >= newHeight-cornerSize) ||
					(x >= newWidth-cornerSize && y >= newHeight-cornerSize) {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				} else {
					// Create irregular dashes using hash for randomness
					if (y < frameWidth || y >= newHeight-frameWidth) && x >= cornerSize && x < newWidth-cornerSize {
						hash := (x * 13) % 17
						dashLength := 4 + (hash % 8) // Random length 4-11
						gapLength := 2 + (hash % 4)  // Random gap 2-5
						total := dashLength + gapLength
						if (x-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
					if (x < frameWidth || x >= newWidth-frameWidth) && y >= cornerSize && y < newHeight-cornerSize {
						hash := (y * 13) % 17
						dashLength := 4 + (hash % 8) // Random length 4-11
						gapLength := 2 + (hash % 4)  // Random gap 2-5
						total := dashLength + gapLength
						if (y-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
				}
			case "dotted":
				// Stamp edge pattern - classic perforated postage stamp look
				perfSpacing := frameWidth
				if perfSpacing < 6 {
					perfSpacing = 6
				}
				perfRadius := frameWidth / 3
				if perfRadius < 2 {
					perfRadius = 2
				}

				// Draw solid border first
				framedImg.Set(x, y, calculateFrameColor(x, y))

				// Cut out perforations (circular holes)
				if y < frameWidth || y >= newHeight-frameWidth {
					// Top/bottom edges
					if x%perfSpacing < perfRadius*2 {
						centerX := (x/perfSpacing)*perfSpacing + perfRadius
						centerY := frameWidth / 2
						if y >= newHeight-frameWidth {
							centerY = newHeight - frameWidth/2
						}
						dx := x - centerX
						dy := y - centerY
						if dx*dx+dy*dy <= perfRadius*perfRadius {
							framedImg.Set(x, y, bgColor) // Cut hole
						}
					}
				} else if x < frameWidth || x >= newWidth-frameWidth {
					// Left/right edges
					if y%perfSpacing < perfRadius*2 {
						centerY := (y/perfSpacing)*perfSpacing + perfRadius
						centerX := frameWidth / 2
						if x >= newWidth-frameWidth {
							centerX = newWidth - frameWidth/2
						}
						dx := x - centerX
						dy := y - centerY
						if dx*dx+dy*dy <= perfRadius*perfRadius {
							framedImg.Set(x, y, bgColor) // Cut hole
						}
					}
				}
			case "dashed":
				// Dashed pattern with proportional corners
				dashLength := frameWidth * 3
				if dashLength < 6 {
					dashLength = 6
				}
				gapLength := dashLength / 2
				total := dashLength + gapLength
				cornerSize := frameWidth

				// Draw solid corners
				if (x < cornerSize && y < cornerSize) ||
					(x >= newWidth-cornerSize && y < cornerSize) ||
					(x < cornerSize && y >= newHeight-cornerSize) ||
					(x >= newWidth-cornerSize && y >= newHeight-cornerSize) {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				} else {
					// Draw dashes on edges
					if (y < frameWidth || y >= newHeight-frameWidth) && x >= cornerSize && x < newWidth-cornerSize {
						if (x-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
					if (x < frameWidth || x >= newWidth-frameWidth) && y >= cornerSize && y < newHeight-cornerSize {
						if (y-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
				}
            case "double":
                // Double border with optional rounded corners.
                // Split frameWidth into outer stroke | gap | inner stroke
                outerWidth := int(math.Max(2, math.Round(float64(frameWidth)*0.4)))
                gapWidth := int(math.Max(1, math.Round(float64(frameWidth)*0.2)))
                innerWidth := frameWidth - outerWidth - gapWidth
                if innerWidth < 1 { innerWidth = 1 }
                // Bias inner stroke thicker without changing outer weight:
                // move a small delta from the gap to the inner band.
                delta := int(math.Max(1, math.Round(float64(frameWidth)*0.1)))
                if gapWidth > delta {
                    gapWidth -= delta
                    innerWidth += delta
                } else if gapWidth > 1 { // ensure at least 1px gap remains
                    innerWidth += (gapWidth - 1)
                    gapWidth = 1
                }
                // Adjust band balance depending on rounded vs straight.
                // Rounded: widen the transparent gap slightly by borrowing from the outer band
                // (keeps the current rounded look you liked).
                // Straight: make the outer band a bit thicker by borrowing from the gap.
                if strings.HasPrefix(frameType, "rounded-") {
                    deltaGap := int(math.Max(1, math.Round(float64(frameWidth)*0.1)))
                    if outerWidth > deltaGap+1 { // leave at least 1px outer stroke
                        outerWidth -= deltaGap
                        gapWidth += deltaGap
                    }
                } else {
                    deltaOuter := int(math.Max(1, math.Round(float64(frameWidth)*0.1)))
                    if gapWidth > deltaOuter { // prefer to reduce gap first
                        gapWidth -= deltaOuter
                        outerWidth += deltaOuter
                    } else if gapWidth > 1 { // ensure at least 1px gap remains
                        outerWidth += (gapWidth - 1)
                        gapWidth = 1
                    } else if innerWidth > 1 { // as a last resort, borrow from inner minimally
                        outerWidth += 1
                        innerWidth -= 1
                    }
                    // Fine-tune: give the gap +1px from the inner band if available
                    // to restore a tiny breathing room between strokes.
                    if innerWidth > 2 {
                        innerWidth -= 1
                        gapWidth += 1
                    }
                }

                if strings.HasPrefix(frameType, "rounded-") {
                    // Rounded classification using rounded rectangles relative to the inner boundary.
                    innerL, innerT := frameWidth, frameWidth
                    innerRgt, innerBtm := newWidth-1-frameWidth, newHeight-1-frameWidth
                    baseR := int(math.Round(float64(frameWidth)*0.55)) // approximate inner corner radius

                    // Offsets from inner boundary for rings
                    offIn := innerWidth
                    offGap := innerWidth + gapWidth
                    offOut := innerWidth + gapWidth + outerWidth // should equal frameWidth

                    // Precompute expanded boxes and radii
                    clamp := func(v, lo, hi int) int { if v < lo { return lo }; if v > hi { return hi }; return v }

                    // Inner stroke outer edge
                    inL := clamp(innerL-offIn, 0, newWidth-1)
                    inT := clamp(innerT-offIn, 0, newHeight-1)
                    inR := clamp(innerRgt+offIn, 0, newWidth-1)
                    inB := clamp(innerBtm+offIn, 0, newHeight-1)
                    rIn := baseR + offIn

                    // Gap outer edge
                    gL := clamp(innerL-offGap, 0, newWidth-1)
                    gT := clamp(innerT-offGap, 0, newHeight-1)
                    gR := clamp(innerRgt+offGap, 0, newWidth-1)
                    gB := clamp(innerBtm+offGap, 0, newHeight-1)
                    rGap := baseR + offGap

                    // Outer stroke outer edge (close to image bounds)
                    oL := clamp(innerL-offOut, 0, newWidth-1)
                    oT := clamp(innerT-offOut, 0, newHeight-1)
                    oR := clamp(innerRgt+offOut, 0, newWidth-1)
                    oB := clamp(innerBtm+offOut, 0, newHeight-1)
                    rOut := baseR + offOut

                    // Membership tests
                    inInnerCore := insideRoundedRect(x, y, innerL, innerT, innerRgt, innerBtm, baseR)
                    inInnerBand := insideRoundedRect(x, y, inL, inT, inR, inB, rIn) && !inInnerCore
                    inGapBand := insideRoundedRect(x, y, gL, gT, gR, gB, rGap) && !insideRoundedRect(x, y, inL, inT, inR, inB, rIn)
                    inOuterBand := insideRoundedRect(x, y, oL, oT, oR, oB, rOut) && !insideRoundedRect(x, y, gL, gT, gR, gB, rGap)

                    if inOuterBand {
                        framedImg.Set(x, y, calculateFrameColor(x, y))
                    } else if inGapBand {
                        framedImg.Set(x, y, bgColor)
                    } else if inInnerBand {
                        framedImg.Set(x, y, calculateFrameColor(x, y))
                    }
                } else {
                    // Straight double using edge-distance bands
                    edgeDist := min4(x, y, newWidth-1-x, newHeight-1-y)
                    if edgeDist < outerWidth {
                        framedImg.Set(x, y, calculateFrameColor(x, y))
                    } else if edgeDist < outerWidth+gapWidth {
                        framedImg.Set(x, y, bgColor)
                    } else if edgeDist < outerWidth+gapWidth+innerWidth {
                        framedImg.Set(x, y, calculateFrameColor(x, y))
                    }
                }
			case "diagonal":
				// Diagonal lines pattern
				// Increase stroke thickness so lines are more visible
				lineSpacing := frameWidth / 2
				if lineSpacing < 2 {
					lineSpacing = 2
				}
				thickness := frameWidth / 5
				if thickness < 2 {
					thickness = 2
				}
				if thickness >= lineSpacing {
					thickness = lineSpacing - 1
					if thickness < 1 { thickness = 1 }
				}
				if (x+y)%lineSpacing < thickness {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				}
			case "grid":
				// Grid pattern - small squares in checkerboard
				gridSize := frameWidth / 3
				if gridSize < 2 {
					gridSize = 2
				}

				// Create checkerboard pattern
				gridX := x / gridSize
				gridY := y / gridSize

				// Alternating squares
				if (gridX+gridY)%2 == 0 {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				}
			default:
				// Simple
				framedImg.Set(x, y, calculateFrameColor(x, y))
			}
		}
	}

	// Draw original QR code in center FIRST
	draw.Draw(framedImg, image.Rect(frameWidth, frameWidth, frameWidth+bounds.Dx(), frameWidth+bounds.Dy()), qrImg, bounds.Min, draw.Src)

	// Apply rounded corners if frame type starts with "rounded-"
	if strings.HasPrefix(frameType, "rounded-") {
		h.applySimpleRoundedFrame(framedImg, frameWidth, bgColor)
	}

	// Save the framed image
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create framed output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, framedImg); err != nil {
		return fmt.Errorf("failed to encode framed PNG: %v", err)
	}

	return nil
}

// addLogoToOriginalQR adds logo to original QR before any padding/frame modifications
func (h *Handler) addLogoToOriginalQR(filename string, fgColor, bgColor color.RGBA, originalSize int, useGradient bool, gradientStart, gradientMiddle, gradientEnd color.RGBA, qrShape string, branding string, customDomain string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	brandedImg := image.NewRGBA(bounds)
	draw.Draw(brandedImg, bounds, qrImg, image.Point{}, draw.Src)

	// Load logo sized for original QR
	maxLogoWidth := originalSize / 3  // 33% of QR width
	maxLogoHeight := originalSize / 5 // Maintain aspect

	// Check branding options
	if branding == "none" {
		return nil // No branding requested
	}

	var logoImg image.Image
	var logoErr error

	if customDomain != "" && branding == "custom" {
		// Generate custom domain SVG text
		logoImg, logoErr = h.generateCustomDomainSVG(customDomain, maxLogoWidth, maxLogoHeight, fgColor)
		if logoErr != nil {
			return fmt.Errorf("failed to generate custom domain SVG: %v", logoErr)
		}
    } else if branding == "default" {
        // Try default branding (SVG only); if missing, skip silently
        logoImg, logoErr = h.loadLogoImage(maxLogoWidth, maxLogoHeight)
        if logoErr != nil || logoImg == nil {
            return nil
        }
	} else {
		return nil // No valid branding option
	}

	logoBounds := logoImg.Bounds()
	logoWidth := logoBounds.Dx()
	logoHeight := logoBounds.Dy()

	// Position logo near the bottom-right corner with small bottom margin
	marginX := originalSize/45 + 1 // Slightly increased horizontal margin + 1 pixel
	marginY := 8                   // Slightly increased bottom margin - a few more pixels up from bottom
	x := bounds.Dx() - logoWidth - marginX
	y := bounds.Dy() - logoHeight - marginY

	// Smart logo area clearing with gradient fade-out for all shapes
	padding := marginX // Reduce padding back to 1x margin for tighter fade area
	h.smartClearLogoArea(brandedImg, x, y, logoWidth, logoHeight, padding, bgColor, qrShape, bounds)

	// Draw logo
	for py := 0; py < logoHeight; py++ {
		for px := 0; px < logoWidth; px++ {
			logoColor := logoImg.At(logoBounds.Min.X+px, logoBounds.Min.Y+py)
			if _, _, _, a := logoColor.RGBA(); a > 32768 {
				if x+px >= 0 && y+py >= 0 && x+px < bounds.Dx() && y+py < bounds.Dy() {
					var pixelColor color.RGBA
					if useGradient {
						// Calculate gradient color based on position within the logo
						gradientX := float64(px) / float64(logoWidth)
						gradientY := float64(py) / float64(logoHeight)
						// Use diagonal gradient (combine x and y)
						t := (gradientX + gradientY) / 2.0
						pixelColor = h.interpolateGradientColor(t, gradientStart, gradientMiddle, gradientEnd)
					} else {
						pixelColor = fgColor
					}
					brandedImg.Set(x+px, y+py, pixelColor)
				}
			}
		}
	}

	// Save the branded image
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, brandedImg); err != nil {
		return fmt.Errorf("failed to encode branded PNG: %v", err)
	}

	return nil
}

// applySimpleRoundedFrame applies rounded corners to frame areas only
func (h *Handler) applySimpleRoundedFrame(img *image.RGBA, frameWidth int, bgColor color.RGBA) {
    bounds := img.Bounds()
    width := bounds.Dx()
    height := bounds.Dy()
    // Choose radii so inner radius stays > 0 to create a rounded inner edge.
    // Make inner radius proportional to frame width, then derive outer radius
    // to keep a uniform stroke thickness (outerR - innerR == frameWidth).
    innerR := int(math.Max(2, math.Round(float64(frameWidth)*0.55)))
    outerR := innerR + frameWidth

    // Colors to clear
    outerClear := color.RGBA{0, 0, 0, 0} // outside outer rounded rect: transparent
    innerClear := color.RGBA{255, 255, 255, 255}
    if bgColor.A == 0 {
        innerClear = color.RGBA{0, 0, 0, 0}
    }

    // Rounded rectangle hit-test
    insideRoundedRect := func(x, y, left, top, right, bottom, r int) bool {
        if r <= 0 {
            return x >= left && x <= right && y >= top && y <= bottom
        }
        // Central bands
        if x >= left+r && x <= right-r && y >= top && y <= bottom {
            return true
        }
        if y >= top+r && y <= bottom-r && x >= left && x <= right {
            return true
        }
        // Corner circles
        // Top-left
        dx := x - (left + r)
        dy := y - (top + r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        // Top-right
        dx = x - (right - r)
        dy = y - (top + r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        // Bottom-left
        dx = x - (left + r)
        dy = y - (bottom - r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        // Bottom-right
        dx = x - (right - r)
        dy = y - (bottom - r)
        if dx*dx+dy*dy <= r*r {
            return true
        }
        return false
    }

    // Define outer and inner rounded rectangles (inclusive coordinates)
    outerL, outerT := 0, 0
    outerRgt, outerBtm := width-1, height-1
    innerL, innerT := frameWidth, frameWidth
    innerRgt, innerBtm := width-1-frameWidth, height-1-frameWidth

    // Carve a full inner sub-stroke with rounded ends by expanding the inner
    // rounded rectangle outward by a cut thickness. This removes a uniform
    // strip from the inner side of the frame, and rounds its corners.
    // Carve thickness: remove roughly a third of the band from the inner side
    // so the final rounded stroke remains bold.
    cut := int(math.Max(2, math.Ceil(float64(frameWidth)*0.33)))
    carveL, carveT := innerL-cut, innerT-cut
    carveRgt, carveBtm := innerRgt+cut, innerBtm+cut
    if carveL < 0 { carveL = 0 }
    if carveT < 0 { carveT = 0 }
    if carveRgt > width-1 { carveRgt = width-1 }
    if carveBtm > height-1 { carveBtm = height-1 }
    carveRadius := innerR + cut

    for y := 0; y < height; y++ {
        for x := 0; x < width; x++ {
            // Only modify the frame band, never touch the QR center
            inFrame := x < frameWidth || x >= width-frameWidth || y < frameWidth || y >= height-frameWidth
            if !inFrame {
                continue
            }

            // Clear anything outside the outer rounded rectangle (outer corners)
            if !insideRoundedRect(x, y, outerL, outerT, outerRgt, outerBtm, outerR) {
                img.Set(x, y, outerClear)
                continue
            }
            // Carve a sub-stroke from the inner side with rounded corners
            if insideRoundedRect(x, y, carveL, carveT, carveRgt, carveBtm, carveRadius) {
                img.Set(x, y, innerClear)
                continue
            }
            // Else keep the pixel: it's part of the stroke (outer − inner)
        }
    }
}

// applyQRBackground replaces transparent background with proper QR background
func (h *Handler) applyQRBackground(filename string, bgColor color.RGBA) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	resultImg := image.NewRGBA(bounds)

	// Apply background only to QR area
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			origColor := qrImg.At(x, y)
			r, g, b, a := origColor.RGBA()
			if a == 0 { // Transparent pixel
				resultImg.Set(x, y, bgColor)
			} else {
				resultImg.Set(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
			}
		}
	}

	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, resultImg); err != nil {
		return fmt.Errorf("failed to encode PNG: %v", err)
	}

	return nil
}

// applyFinalBackground applies background to entire image, preserving transparency
func (h *Handler) applyFinalBackground(filename string, bgColor color.RGBA) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}

	img, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode image: %v", err)
	}

	bounds := img.Bounds()
	resultImg := image.NewRGBA(bounds)

	// Fill entire image with background, then overlay original
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			resultImg.Set(x, y, bgColor)
		}
	}

	// Draw original image on top (preserving non-transparent areas)
	draw.Draw(resultImg, bounds, img, bounds.Min, draw.Over)

	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, resultImg); err != nil {
		return fmt.Errorf("failed to encode PNG: %v", err)
	}

	return nil
}

// interpolateGradientColor calculates color at position t (0.0 to 1.0) in gradient
func (h *Handler) interpolateGradientColor(t float64, gradientStart, gradientMiddle, gradientEnd color.RGBA) color.RGBA {
	// Clamp t to valid range
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}

	// Interpolate between the 3 gradient colors
	if t <= 0.5 {
		// Interpolate between start (t=0) and middle (t=0.5)
		localT := t * 2.0 // Scale to 0-1 range for this segment
		return h.lerpColor(gradientStart, gradientMiddle, localT)
	} else {
		// Interpolate between middle (t=0.5) and end (t=1.0)
		localT := (t - 0.5) * 2.0 // Scale to 0-1 range for this segment
		return h.lerpColor(gradientMiddle, gradientEnd, localT)
	}
}

// lerpColor performs linear interpolation between two colors
func (h *Handler) lerpColor(color1, color2 color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(color1.R) + t*(float64(color2.R)-float64(color1.R))),
		G: uint8(float64(color1.G) + t*(float64(color2.G)-float64(color1.G))),
		B: uint8(float64(color1.B) + t*(float64(color2.B)-float64(color1.B))),
		A: 255,
	}
}

// addSVGBrandingOverlay adds SVG branding to bottom-right of original QR area
func (h *Handler) addSVGBrandingOverlay(filename string, fgColor, bgColor color.RGBA, originalSize, borderPercent, frameWidthPercent int, hasFrame bool, useGradient bool, gradient *standard.LinearGradient, gradientStart, gradientMiddle, gradientEnd color.RGBA, qrShape string, branding string, customDomain string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	brandedImg := image.NewRGBA(bounds)
	draw.Draw(brandedImg, bounds, qrImg, image.Point{}, draw.Src)

	// Simple approach: position logo at exact bottom-right of original QR
	// Calculate scale factor from original to current size
	scaleFactor := float64(bounds.Dx()) / float64(originalSize)

	// Check branding options
	if branding == "none" {
		return nil // No branding requested
	}

	// Load logo at fixed size relative to original QR
	// Use same size calculations as PNG version for consistency
	maxLogoWidth := originalSize / 3  // 33% of QR width (match PNG)
	maxLogoHeight := originalSize / 5 // Maintain aspect (match PNG)

	var logoImg image.Image
	var logoErr error

	if customDomain != "" && branding == "custom" {
		// Generate custom domain SVG text
		logoImg, logoErr = h.generateCustomDomainSVG(customDomain, maxLogoWidth, maxLogoHeight, fgColor)
		if logoErr != nil {
			return fmt.Errorf("failed to generate custom domain SVG: %v", logoErr)
		}
	} else if branding == "default" {
		// Use default shortn.link branding
		logoImg, logoErr = h.loadLogoImage(maxLogoWidth, maxLogoHeight)
		if logoErr != nil {
			return fmt.Errorf("failed to load SVG branding logo: %v", logoErr)
		}
	} else {
		return nil // No valid branding option
	}

	// Scale logo to current resolution
	logoBounds := logoImg.Bounds()
	scaledWidth := int(float64(logoBounds.Dx()) * scaleFactor)
	scaledHeight := int(float64(logoBounds.Dy()) * scaleFactor)

	// Calculate QR area boundaries correctly
	paddingPixels := int(float64(originalSize*borderPercent/100) * scaleFactor)
	framePixels := 0
	if hasFrame {
		framePixels = int(float64(originalSize*frameWidthPercent/100) * scaleFactor)
	}

	// QR area boundaries in final image (corrected)
	qrLeft := framePixels + paddingPixels
	qrTop := framePixels + paddingPixels
	// QR size in final image is the remaining space after removing padding and frame
	actualQRSize := bounds.Dx() - (framePixels+paddingPixels)*2
	qrRight := qrLeft + actualQRSize
	qrBottom := qrTop + actualQRSize

	// Position logo near bottom-right corner WITHIN the QR area
	// Use same margin calculations as PNG version for consistency
	marginX := int(float64(originalSize/45+1) * scaleFactor) // Match PNG dynamic margin calculation
	marginY := int(8 * scaleFactor)                          // Match PNG fixed margin
	x := qrRight - scaledWidth - marginX
	y := qrBottom - scaledHeight - marginY

	// Ensure logo stays within image bounds
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x+scaledWidth > bounds.Dx() {
		x = bounds.Dx() - scaledWidth
	}
	if y+scaledHeight > bounds.Dy() {
		y = bounds.Dy() - scaledHeight
	}

	// Debug: print position info
	fmt.Printf("QR area: left=%d, top=%d, right=%d, bottom=%d\n", qrLeft, qrTop, qrRight, qrBottom)
	fmt.Printf("Logo position: x=%d, y=%d, size=%dx%d, bounds=%dx%d\n", x, y, scaledWidth, scaledHeight, bounds.Dx(), bounds.Dy())

	// Smart logo area clearing with stroke completion for organic shapes
	// Use same padding calculation as PNG version for consistency
	padding := marginX
	h.smartClearLogoArea(brandedImg, x, y, scaledWidth, scaledHeight, padding, bgColor, qrShape, bounds)

	// Draw scaled SVG logo
	for py := 0; py < scaledHeight; py++ {
		for px := 0; px < scaledWidth; px++ {
			// Sample from original logo with scaling
			origX := int(float64(px) / scaleFactor)
			origY := int(float64(py) / scaleFactor)
			if origX >= 0 && origY >= 0 && origX < logoBounds.Dx() && origY < logoBounds.Dy() {
				logoColor := logoImg.At(logoBounds.Min.X+origX, logoBounds.Min.Y+origY)
				if _, _, _, a := logoColor.RGBA(); a > 32768 {
					if x+px >= 0 && y+py >= 0 && x+px < bounds.Max.X && y+py < bounds.Max.Y {
						var pixelColor color.RGBA
						if useGradient && gradient != nil {
							// Calculate gradient color based on position within the logo
							gradientX := float64(px) / float64(scaledWidth)
							gradientY := float64(py) / float64(scaledHeight)
							// Use diagonal gradient (combine x and y)
							t := (gradientX + gradientY) / 2.0
							pixelColor = h.interpolateGradientColor(t, gradientStart, gradientMiddle, gradientEnd)
						} else {
							pixelColor = fgColor
						}
						brandedImg.Set(x+px, y+py, pixelColor)
					}
				}
			}
		}
	}

	// Save the branded image
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, brandedImg); err != nil {
		return fmt.Errorf("failed to encode branded PNG: %v", err)
	}

	return nil
}

// customShape implements the IShape interface by wrapping drawing functions from the shapes package
type customShape struct {
	drawFunc func(ctx *standard.DrawContext)
}

// Draw implements the IShape interface
func (cs *customShape) Draw(ctx *standard.DrawContext) {
	cs.drawFunc(ctx)
}

// DrawFinder implements the IShape interface for finder patterns
func (cs *customShape) DrawFinder(ctx *standard.DrawContext) {
	// Use the same drawing function for finder patterns
	cs.drawFunc(ctx)
}

// smartClearLogoArea intelligently clears the logo area with shape-aware stroke completion
func (h *Handler) smartClearLogoArea(img *image.RGBA, logoX, logoY, logoWidth, logoHeight, padding int, bgColor color.RGBA, qrShape string, bounds image.Rectangle) {
	fmt.Printf("DEBUG: smartClearLogoArea called with qrShape='%s'\n", qrShape)

	// Apply gradient fade-out to all shapes, including rectangle
	if qrShape == "rectangle" {
		fmt.Printf("DEBUG: Using gradient fade-out for rectangle shape\n")
		h.createGradientFadeOut(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
		return
	}

	// For organic shapes, implement intelligent stroke completion
	switch qrShape {
	case "circle":
		fmt.Printf("DEBUG: Using circle completion\n")
		h.clearLogoAreaWithCircleCompletion(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
	case "liquid":
		fmt.Printf("DEBUG: Using liquid completion\n")
		h.clearLogoAreaWithLiquidCompletion(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
	case "chain":
		fmt.Printf("DEBUG: Using chain completion\n")
		h.clearLogoAreaWithChainCompletion(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
	case "hstripe", "vstripe":
		fmt.Printf("DEBUG: Using stripe completion for %s\n", qrShape)
		h.clearLogoAreaWithStripeCompletion(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds, qrShape)
	default:
		fmt.Printf("DEBUG: Using fallback simple clearing for unknown shape: %s\n", qrShape)
		h.simpleClearLogoArea(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor)
	}
}

// simpleClearLogoArea performs basic rectangular clearing
func (h *Handler) simpleClearLogoArea(img *image.RGBA, logoX, logoY, logoWidth, logoHeight, padding int, bgColor color.RGBA) {
	for py := logoY - padding; py < logoY+logoHeight+padding; py++ {
		for px := logoX - padding; px < logoX+logoWidth+padding; px++ {
			if px >= 0 && py >= 0 && px < img.Bounds().Max.X && py < img.Bounds().Max.Y {
				img.Set(px, py, bgColor)
			}
		}
	}
}

// clearLogoAreaWithCircleCompletion creates gradient fade-out for circular strokes
func (h *Handler) clearLogoAreaWithCircleCompletion(img *image.RGBA, logoX, logoY, logoWidth, logoHeight, padding int, bgColor color.RGBA, bounds image.Rectangle) {
	h.createGradientFadeOut(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
}

// createGradientFadeOut creates a simple clear area with rounded corners around the logo
func (h *Handler) createGradientFadeOut(img *image.RGBA, logoX, logoY, logoWidth, logoHeight, padding int, bgColor color.RGBA, bounds image.Rectangle) {
	// Clear the logo area and padding with rounded corners
	for py := logoY - padding; py < logoY+logoHeight+padding; py++ {
		for px := logoX - padding; px < logoX+logoWidth+padding; px++ {
			if px < 0 || py < 0 || px >= bounds.Max.X || py >= bounds.Max.Y {
				continue
			}

			// Calculate distance from logo edge for rounded corners
			distX := 0.0
			distY := 0.0

			if px < logoX {
				distX = float64(logoX - px)
			} else if px >= logoX+logoWidth {
				distX = float64(px - (logoX + logoWidth - 1))
			}

			if py < logoY {
				distY = float64(logoY - py)
			} else if py >= logoY+logoHeight {
				distY = float64(py - (logoY + logoHeight - 1))
			}

			// Use rectangular distance for sharp corners
			distance := math.Max(distX, distY)

			// Clear area within padding distance (including logo area itself)
			if distance <= float64(padding) || (px >= logoX && px < logoX+logoWidth && py >= logoY && py < logoY+logoHeight) {
				img.Set(px, py, bgColor)
			}
		}
	}
}

// cleanupAntiAliasing removes white border pixels caused by anti-aliasing
func (h *Handler) cleanupAntiAliasing(filename string, fgColor color.RGBA) error {
	// Open and decode the image
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("failed to decode image: %v", err)
	}

	// Convert to RGBA for manipulation
	bounds := img.Bounds()
	cleanImg := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			origColor := img.At(x, y)
			r, g, b, a := origColor.RGBA()

			// Convert back to 8-bit values
			r8, g8, b8, a8 := uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)

			// Check if this is a problematic anti-aliasing pixel
			if h.isAntiAliasingArtifact(r8, g8, b8, a8, fgColor) {
				// Make it fully transparent
				cleanImg.Set(x, y, color.RGBA{0, 0, 0, 0})
			} else {
				// Keep the original pixel
				cleanImg.Set(x, y, color.RGBA{r8, g8, b8, a8})
			}
		}
	}

	// Save the cleaned image back to file
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, cleanImg); err != nil {
		return fmt.Errorf("failed to encode cleaned image: %v", err)
	}

	return nil
}

// isAntiAliasingArtifact detects semi-transparent white/gray pixels that are anti-aliasing artifacts
func (h *Handler) isAntiAliasingArtifact(r, g, b, a uint8, fgColor color.RGBA) bool {
	// Skip fully transparent pixels
	if a == 0 {
		return false
	}

	// Skip fully opaque pixels that match the foreground color (these are real QR pixels)
	if a == 255 && r == fgColor.R && g == fgColor.G && b == fgColor.B {
		return false
	}

	// Semi-transparent pixels are likely anti-aliasing artifacts
	if a < 255 {
		return true
	}

	// Light gray/white pixels that aren't the exact foreground color are likely artifacts
	if (r > 200 && g > 200 && b > 200) && (r != fgColor.R || g != fgColor.G || b != fgColor.B) {
		return true
	}

	return false
}

// colorsAreSimilar checks if two colors are within a threshold distance
func (h *Handler) colorsAreSimilar(c1, c2 color.RGBA, threshold int) bool {
	dr := int(c1.R) - int(c2.R)
	dg := int(c1.G) - int(c2.G)
	db := int(c1.B) - int(c2.B)

	if dr < 0 {
		dr = -dr
	}
	if dg < 0 {
		dg = -dg
	}
	if db < 0 {
		db = -db
	}

	return dr < threshold && dg < threshold && db < threshold
}

// interpolateColors linearly interpolates between two colors
func (h *Handler) interpolateColors(from, to color.RGBA, factor float64) color.RGBA {
	if factor < 0 {
		factor = 0
	}
	if factor > 1 {
		factor = 1
	}

	invFactor := 1.0 - factor

	return color.RGBA{
		R: uint8(float64(from.R)*invFactor + float64(to.R)*factor),
		G: uint8(float64(from.G)*invFactor + float64(to.G)*factor),
		B: uint8(float64(from.B)*invFactor + float64(to.B)*factor),
		A: 255,
	}
}

// clearLogoAreaWithLiquidCompletion creates gradient fade-out for liquid shapes
func (h *Handler) clearLogoAreaWithLiquidCompletion(img *image.RGBA, logoX, logoY, logoWidth, logoHeight, padding int, bgColor color.RGBA, bounds image.Rectangle) {
	h.createGradientFadeOut(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
}

// clearLogoAreaWithChainCompletion creates gradient fade-out for chain shapes
func (h *Handler) clearLogoAreaWithChainCompletion(img *image.RGBA, logoX, logoY, logoWidth, logoHeight, padding int, bgColor color.RGBA, bounds image.Rectangle) {
	h.createGradientFadeOut(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
}

// clearLogoAreaWithStripeCompletion creates gradient fade-out for stripe shapes
func (h *Handler) clearLogoAreaWithStripeCompletion(img *image.RGBA, logoX, logoY, logoWidth, logoHeight, padding int, bgColor color.RGBA, bounds image.Rectangle, qrShape string) {
	h.createGradientFadeOut(img, logoX, logoY, logoWidth, logoHeight, padding, bgColor, bounds)
}

// Helper functions for stroke completion algorithms

func (h *Handler) extractQRColor(img *image.RGBA, logoX, logoY int, bounds image.Rectangle) color.RGBA {
	// Sample a few pixels around the logo area to find the QR foreground color
	for dy := -10; dy <= 10; dy += 5 {
		for dx := -10; dx <= 10; dx += 5 {
			px, py := logoX+dx, logoY+dy
			if px >= 0 && py >= 0 && px < bounds.Max.X && py < bounds.Max.Y {
				c := img.RGBAAt(px, py)
				// Look for non-background colors
				if c.R < 200 || c.G < 200 || c.B < 200 { // Assuming light backgrounds
					return c
				}
			}
		}
	}
	return color.RGBA{0, 0, 0, 255} // Default to black
}

func (h *Handler) isQRForegroundColor(pixelColor, qrColor, bgColor color.RGBA) bool {
	// Check if pixel color is similar to QR foreground (not background)
	threshold := uint32(50)
	dr := uint32(pixelColor.R) - uint32(qrColor.R)
	dg := uint32(pixelColor.G) - uint32(qrColor.G)
	db := uint32(pixelColor.B) - uint32(qrColor.B)
	distance := dr*dr + dg*dg + db*db
	return distance < threshold*threshold
}

func (h *Handler) blendColors(color1, color2 color.RGBA, ratio float64) color.RGBA {
	if ratio <= 0 {
		return color2
	}
	if ratio >= 1 {
		return color1
	}
	return color.RGBA{
		R: uint8(float64(color1.R)*ratio + float64(color2.R)*(1-ratio)),
		G: uint8(float64(color1.G)*ratio + float64(color2.G)*(1-ratio)),
		B: uint8(float64(color1.B)*ratio + float64(color2.B)*(1-ratio)),
		A: 255,
	}
}

func (h *Handler) distanceToLogoArea(px, py, logoX, logoY, logoWidth, logoHeight int) float64 {
	// Calculate minimum distance from point to logo rectangle
	dx := max(0, max(logoX-px, px-(logoX+logoWidth)))
	dy := max(0, max(logoY-py, py-(logoY+logoHeight)))
	return math.Sqrt(float64(dx*dx + dy*dy))
}

func (h *Handler) generateLiquidCompletion(px, py, logoX, logoY, logoWidth, logoHeight, padding int) float64 {
	// Generate organic liquid-like completion curves
	distToEdge := h.distanceToLogoArea(px, py, logoX, logoY, logoWidth, logoHeight)
	if distToEdge > float64(padding) {
		return 0
	}

	// Create flowing curves using sine waves for organic feel
	normalizedDist := distToEdge / float64(padding)
	angle := math.Atan2(float64(py-logoY-logoHeight/2), float64(px-logoX-logoWidth/2))
	waveInfluence := math.Sin(angle*3) * 0.3 // Create organic variations

	completion := (1.0 - normalizedDist) * (0.7 + waveInfluence)
	return math.Max(0, completion)
}

func (h *Handler) generateChainCompletion(px, py, logoX, logoY, logoWidth, logoHeight, padding int) float64 {
	// For chain links, create circular completions at regular intervals
	distToEdge := h.distanceToLogoArea(px, py, logoX, logoY, logoWidth, logoHeight)
	if distToEdge > float64(padding) {
		return 0
	}

	// Create circular chain link completions
	centerX := logoX + logoWidth/2
	centerY := logoY + logoHeight/2
	dx := float64(px - centerX)
	dy := float64(py - centerY)
	distFromCenter := math.Sqrt(dx*dx + dy*dy)

	// Create chain links at regular intervals
	linkRadius := float64(padding) / 3
	linkSpacing := linkRadius * 2
	linkPhase := math.Mod(distFromCenter, linkSpacing)

	if linkPhase < linkRadius {
		normalizedDist := distToEdge / float64(padding)
		return 1.0 - normalizedDist
	}
	return 0
}

func (h *Handler) generateStripeCompletion(px, py, logoX, logoY, logoWidth, logoHeight, padding int, isHorizontal bool) float64 {
	distToEdge := h.distanceToLogoArea(px, py, logoX, logoY, logoWidth, logoHeight)
	if distToEdge > float64(padding) {
		return 0
	}

	// Extend stripes in the appropriate direction
	var stripePosition int
	if isHorizontal {
		stripePosition = py
	} else {
		stripePosition = px
	}

	// Create stripe pattern with smooth transitions
	stripeWidth := padding / 2
	if stripeWidth < 2 {
		stripeWidth = 2
	}

	if stripePosition%stripeWidth < stripeWidth/2 {
		normalizedDist := distToEdge / float64(padding)
		return 1.0 - normalizedDist
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// QRElement represents a QR shape element (circle, liquid blob, etc)
type QRElement struct {
	centerX, centerY int
	radius           float64
	elementType      string // "circle", "liquid", etc
}

// identifyQRElementsIntersectingLogo finds QR elements that intersect with the logo area
func (h *Handler) identifyQRElementsIntersectingLogo(img *image.RGBA, logoX, logoY, logoWidth, logoHeight int, qrColor, bgColor color.RGBA, bounds image.Rectangle) []QRElement {
	var cutElements []QRElement

	// Define padding area around logo
	padding := max(logoWidth, logoHeight) / 4

	// Scan the logo area and its immediate surroundings to find QR elements that intersect
	for py := logoY - padding; py < logoY+logoHeight+padding; py++ {
		for px := logoX - padding; px < logoX+logoWidth+padding; px++ {
			if px < 0 || py < 0 || px >= bounds.Max.X || py >= bounds.Max.Y {
				continue
			}

			currentColor := img.RGBAAt(px, py)

			// If we find a QR foreground pixel
			if h.isQRForegroundColor(currentColor, qrColor, bgColor) {
				// Check if this pixel has part inside the logo area and part outside
				if h.isQRElementCutByLogo(px, py, logoX, logoY, logoWidth, logoHeight, img, qrColor, bgColor, bounds) {
					// Estimate the center and radius of this QR element
					centerX, centerY, radius := h.estimateQRElementGeometry(px, py, img, qrColor, bgColor, bounds)

					// Add to cut elements if not already present
					isNew := true
					for _, existing := range cutElements {
						if math.Abs(float64(existing.centerX-centerX)) < radius/2 && math.Abs(float64(existing.centerY-centerY)) < radius/2 {
							isNew = false
							break
						}
					}

					if isNew {
						cutElements = append(cutElements, QRElement{
							centerX:     centerX,
							centerY:     centerY,
							radius:      radius,
							elementType: "circle",
						})
					}
				}
			}
		}
	}

	return cutElements
}

// isQRElementCutByLogo checks if a QR element at this position is cut by the logo
func (h *Handler) isQRElementCutByLogo(px, py, logoX, logoY, logoWidth, logoHeight int, img *image.RGBA, qrColor, bgColor color.RGBA, bounds image.Rectangle) bool {
	// Look in a small radius around this pixel to see if there are both QR pixels and logo area pixels
	searchRadius := 8
	hasQRPixels := false
	hasLogoAreaPixels := false

	for dy := -searchRadius; dy <= searchRadius; dy++ {
		for dx := -searchRadius; dx <= searchRadius; dx++ {
			checkX, checkY := px+dx, py+dy
			if checkX < 0 || checkY < 0 || checkX >= bounds.Max.X || checkY >= bounds.Max.Y {
				continue
			}

			// Check if this pixel is in the logo area
			if checkX >= logoX && checkX < logoX+logoWidth && checkY >= logoY && checkY < logoY+logoHeight {
				hasLogoAreaPixels = true
			} else {
				// Check if this pixel is QR foreground
				pixelColor := img.RGBAAt(checkX, checkY)
				if h.isQRForegroundColor(pixelColor, qrColor, bgColor) {
					hasQRPixels = true
				}
			}

			if hasQRPixels && hasLogoAreaPixels {
				return true
			}
		}
	}

	return false
}

// estimateQRElementGeometry estimates the center and size of a QR element
func (h *Handler) estimateQRElementGeometry(startX, startY int, img *image.RGBA, qrColor, bgColor color.RGBA, bounds image.Rectangle) (centerX, centerY int, radius float64) {
	// Use flood fill approach to find the extent of this QR element
	minX, maxX := startX, startX
	minY, maxY := startY, startY

	// Simple expansion to find bounds
	searchRadius := 15
	for dy := -searchRadius; dy <= searchRadius; dy++ {
		for dx := -searchRadius; dx <= searchRadius; dx++ {
			checkX, checkY := startX+dx, startY+dy
			if checkX < 0 || checkY < 0 || checkX >= bounds.Max.X || checkY >= bounds.Max.Y {
				continue
			}

			pixelColor := img.RGBAAt(checkX, checkY)
			if h.isQRForegroundColor(pixelColor, qrColor, bgColor) {
				minX = min(minX, checkX)
				maxX = max(maxX, checkX)
				minY = min(minY, checkY)
				maxY = max(maxY, checkY)
			}
		}
	}

	// Calculate center and radius
	centerX = (minX + maxX) / 2
	centerY = (minY + maxY) / 2
	width := maxX - minX + 1
	height := maxY - minY + 1
	radius = float64(max(width, height)) / 2

	return centerX, centerY, radius
}

// pixelBelongsToCutCircle checks if a pixel belongs to one of the cut circles
func (h *Handler) pixelBelongsToCutCircle(px, py int, cutElements []QRElement, logoX, logoY, logoWidth, logoHeight int) bool {
	for _, element := range cutElements {
		// Calculate distance from pixel to element center
		dx := float64(px - element.centerX)
		dy := float64(py - element.centerY)
		distance := math.Sqrt(dx*dx + dy*dy)

		// Check if pixel is within the circle and should be completed
		if distance <= element.radius {
			// Only complete if this pixel would extend the cut circle naturally
			distToLogo := h.distanceToLogoArea(px, py, logoX, logoY, logoWidth, logoHeight)
			if distToLogo <= element.radius/2 { // Within reasonable completion distance
				return true
			}
		}
	}
	return false
}

// pixelBelongsToCutLiquidElement checks if a pixel belongs to a cut liquid element
func (h *Handler) pixelBelongsToCutLiquidElement(px, py int, cutElements []QRElement, logoX, logoY, logoWidth, logoHeight int) bool {
	for _, element := range cutElements {
		// For liquid elements, use a more organic shape test
		dx := float64(px - element.centerX)
		dy := float64(py - element.centerY)
		distance := math.Sqrt(dx*dx + dy*dy)

		// Check if pixel is within the liquid element bounds
		if distance <= element.radius*1.2 { // Slightly larger radius for organic shapes
			// Only complete if this pixel would extend the cut element naturally
			distToLogo := h.distanceToLogoArea(px, py, logoX, logoY, logoWidth, logoHeight)
			if distToLogo <= element.radius*0.8 { // Within reasonable completion distance
				return true
			}
		}
	}
	return false
}

// createQRWithLogo creates a QR code with a logo embedded in the center
func (h *Handler) createQRWithLogo(content, logoPath, outputPath string, qrSize uint8) error {
	qr, err := qrcode.New(content)
	if err != nil {
		return fmt.Errorf("create qrcode failed: %v", err)
	}

	var options []standard.ImageOption

	// Set QR size first
	options = append(options, standard.WithQRWidth(qrSize))

	// Logo size multiplier must be set before logo to ensure validation passes
	options = append(options, standard.WithLogoSizeMultiplier(2))

	// Determine logo format and add appropriate option
	if strings.HasSuffix(strings.ToLower(logoPath), ".png") {
		options = append(options, standard.WithLogoImageFilePNG(logoPath))
	} else if strings.HasSuffix(strings.ToLower(logoPath), ".jpg") || strings.HasSuffix(strings.ToLower(logoPath), ".jpeg") {
		options = append(options, standard.WithLogoImageFileJPEG(logoPath))
	} else {
		return fmt.Errorf("unsupported logo format: %s", logoPath)
	}

	// Enable safe zone after logo is set
	options = append(options, standard.WithLogoSafeZone())

	writer, err := standard.New(outputPath, options...)
	if err != nil {
		return fmt.Errorf("create writer failed: %v", err)
	}
	defer writer.Close()

	if err = qr.Save(writer); err != nil {
		return fmt.Errorf("save qrcode failed: %v", err)
	}

	return nil
}
