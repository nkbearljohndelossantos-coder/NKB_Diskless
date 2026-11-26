<#
.SYNOPSIS
    Generates NKB Diskless System Branding Assets (Icons, Favicons, Wizard Bitmaps)
.DESCRIPTION
    Processes the user's master branding image into:
    - Multi-resolution Windows application icons (.ico)
    - Browser favicons (.ico, .png, .svg)
    - Inno Setup wizard banners (wizard-large.bmp, wizard-small.bmp)
    - High-resolution web assets (nkb-logo.png, nkb-banner.png)
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$SourceImage = "C:\Users\earlj\.gemini\antigravity\brain\17659249-87ea-4015-b488-bb70eb79f78f\.user_uploaded\media_1795494810239_512541c9.jpg",

    [Parameter(Mandatory = $false)]
    [string]$WorkspaceRoot = $PSScriptRoot
)

if (-not $WorkspaceRoot -or $WorkspaceRoot -eq "") {
    $WorkspaceRoot = (Get-Location).Path
}

Add-Type -AssemblyName System.Drawing

Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "   NKB DISKLESS SYSTEM — BRANDING ASSET GENERATION ENGINE       " -ForegroundColor Cyan
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "Source Image  : $SourceImage"
Write-Host "Workspace Root: $WorkspaceRoot"

if (-not (Test-Path $SourceImage)) {
    throw "Source branding image not found at $SourceImage"
}

$bmp = [System.Drawing.Bitmap]::FromFile($SourceImage)
Write-Host "Loaded Master Image: $($bmp.Width) x $($bmp.Height) px" -ForegroundColor Green

# Target directories
$ResDir = Join-Path $WorkspaceRoot "dist\installer\installer-source\resources"
$PublicDir = Join-Path $WorkspaceRoot "frontend\public"
$AssetsDir = Join-Path $WorkspaceRoot "frontend\src\assets"
$FrontendDistDir = Join-Path $WorkspaceRoot "frontend\dist"

foreach ($dir in @($ResDir, $PublicDir, $AssetsDir, $FrontendDistDir)) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# Helper: Multi-resolution ICO generator
function Export-MultiResIco {
    param(
        [System.Drawing.Bitmap]$Source,
        [System.Drawing.Rectangle]$CropRect,
        [string]$DestinationPath,
        [int[]]$Sizes = @(256, 128, 64, 48, 32, 16)
    )

    $pngStreams = @()
    foreach ($sz in $Sizes) {
        $resized = [System.Drawing.Bitmap]::new($sz, $sz, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $g = [System.Drawing.Graphics]::FromImage($resized)
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
        $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $destRect = [System.Drawing.Rectangle]::new(0, 0, $sz, $sz)
        $g.DrawImage($Source, $destRect, $CropRect, [System.Drawing.GraphicsUnit]::Pixel)
        $g.Dispose()

        $ms = [System.IO.MemoryStream]::new()
        $resized.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
        $resized.Dispose()
        $pngStreams += $ms
    }

    $fs = [System.IO.FileStream]::new($DestinationPath, [System.IO.FileMode]::Create)
    $bw = [System.IO.BinaryWriter]::new($fs)

    # ICONDIR
    $bw.Write([uint16]0) # Reserved
    $bw.Write([uint16]1) # Type 1 = Icon
    $bw.Write([uint16]$Sizes.Count) # Count

    $offset = 6 + (16 * $Sizes.Count)

    for ($i = 0; $i -lt $Sizes.Count; $i++) {
        $sz = $Sizes[$i]
        $w = if ($sz -eq 256) { [byte]0 } else { [byte]$sz }
        $h = if ($sz -eq 256) { [byte]0 } else { [byte]$sz }
        $bytesLen = [uint32]$pngStreams[$i].Length

        $bw.Write($w)
        $bw.Write($h)
        $bw.Write([byte]0) # Color count
        $bw.Write([byte]0) # Reserved
        $bw.Write([uint16]1) # Planes
        $bw.Write([uint16]32) # BitCount
        $bw.Write($bytesLen) # Size
        $bw.Write([uint32]$offset) # Offset

        $offset += $bytesLen
    }

    for ($i = 0; $i -lt $Sizes.Count; $i++) {
        $bytes = $pngStreams[$i].ToArray()
        $bw.Write($bytes)
        $pngStreams[$i].Dispose()
    }

    $bw.Flush()
    $bw.Close()
    $fs.Close()
    Write-Host ("  + Generated Icon: {0} ({1:N0} bytes)" -f (Split-Path $DestinationPath -Leaf), (Get-Item $DestinationPath).Length) -ForegroundColor Green
}

# Crop bounds for Golden NKB Diamond Emblem
$EmblemRect = [System.Drawing.Rectangle]::new(80, 85, 345, 350)

# 1. Generate Windows Application Icon (app.ico)
Write-Host "[1/6] Generating Windows multi-resolution app.ico..." -ForegroundColor Yellow
$AppIcoPath = Join-Path $ResDir "app.ico"
Export-MultiResIco -Source $bmp -CropRect $EmblemRect -DestinationPath $AppIcoPath -Sizes @(256, 128, 64, 48, 32, 16)

# 2. Generate Browser Favicon (favicon.ico and favicon.png)
Write-Host "[2/6] Generating browser favicons..." -ForegroundColor Yellow
$FaviconIcoPath = Join-Path $PublicDir "favicon.ico"
Export-MultiResIco -Source $bmp -CropRect $EmblemRect -DestinationPath $FaviconIcoPath -Sizes @(64, 48, 32, 16)

# Favicon PNG (64x64 and 192x192)
$favPng64 = [System.Drawing.Bitmap]::new(64, 64, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
$gFav = [System.Drawing.Graphics]::FromImage($favPng64)
$gFav.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
$gFav.DrawImage($bmp, [System.Drawing.Rectangle]::new(0, 0, 64, 64), $EmblemRect, [System.Drawing.GraphicsUnit]::Pixel)
$gFav.Dispose()
$favPng64.Save((Join-Path $PublicDir "favicon.png"), [System.Drawing.Imaging.ImageFormat]::Png)
$favPng64.Save((Join-Path $PublicDir "favicon-32x32.png"), [System.Drawing.Imaging.ImageFormat]::Png)
$favPng64.Dispose()
Write-Host "  + Generated favicon.png and favicon-32x32.png" -ForegroundColor Green

# Copy favicons to frontend dist directly as well
Copy-Item (Join-Path $PublicDir "favicon.ico") (Join-Path $FrontendDistDir "favicon.ico") -Force
Copy-Item (Join-Path $PublicDir "favicon.png") (Join-Path $FrontendDistDir "favicon.png") -Force

# 3. Generate Inno Setup Wizard Small Image (55x55 24-bit BMP)
Write-Host "[3/6] Generating Inno Setup WizardSmallImageFile (55x55 BMP)..." -ForegroundColor Yellow
$wizSmall = [System.Drawing.Bitmap]::new(55, 55, [System.Drawing.Imaging.PixelFormat]::Format24bppRgb)
$gSmall = [System.Drawing.Graphics]::FromImage($wizSmall)
$gSmall.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
$gSmall.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
$gSmall.DrawImage($bmp, [System.Drawing.Rectangle]::new(0, 0, 55, 55), $EmblemRect, [System.Drawing.GraphicsUnit]::Pixel)
$gSmall.Dispose()
$WizSmallPath = Join-Path $ResDir "wizard-small.bmp"
$wizSmall.Save($WizSmallPath, [System.Drawing.Imaging.ImageFormat]::Bmp)
$wizSmall.Dispose()
Write-Host "  + Generated: $WizSmallPath" -ForegroundColor Green

# 4. Generate Inno Setup Wizard Large Banner (164x314 24-bit BMP)
Write-Host "[4/6] Generating Inno Setup WizardImageFile (164x314 BMP)..." -ForegroundColor Yellow
$wizLarge = [System.Drawing.Bitmap]::new(164, 314, [System.Drawing.Imaging.PixelFormat]::Format24bppRgb)
$gLarge = [System.Drawing.Graphics]::FromImage($wizLarge)
$gLarge.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
$gLarge.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
$gLarge.Clear([System.Drawing.Color]::FromArgb(10, 18, 40))

# Upper section: Golden NKB Diamond Emblem (w=130, h=130)
$destEmblem = [System.Drawing.Rectangle]::new(17, 12, 130, 130)
$gLarge.DrawImage($bmp, $destEmblem, $EmblemRect, [System.Drawing.GraphicsUnit]::Pixel)

# Lower section: Server rack & glowing client workstations
$srcServer = [System.Drawing.Rectangle]::new(420, 60, 550, 450)
$destServer = [System.Drawing.Rectangle]::new(2, 150, 160, 155)
$gLarge.DrawImage($bmp, $destServer, $srcServer, [System.Drawing.GraphicsUnit]::Pixel)
$gLarge.Dispose()

$WizLargePath = Join-Path $ResDir "wizard-large.bmp"
$wizLarge.Save($WizLargePath, [System.Drawing.Imaging.ImageFormat]::Bmp)
$wizLarge.Dispose()
Write-Host "  + Generated: $WizLargePath" -ForegroundColor Green

# 5. Generate High-Res Logo PNG (nkb-logo.png 512x512)
Write-Host "[5/6] Generating high-resolution logo PNG (512x512)..." -ForegroundColor Yellow
$logoBmp = [System.Drawing.Bitmap]::new(512, 512, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
$gLogo = [System.Drawing.Graphics]::FromImage($logoBmp)
$gLogo.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
$gLogo.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
# Draw emblem with slight margin
$gLogo.DrawImage($bmp, [System.Drawing.Rectangle]::new(16, 16, 480, 480), $EmblemRect, [System.Drawing.GraphicsUnit]::Pixel)
$gLogo.Dispose()

$LogoResPath = Join-Path $ResDir "nkb-logo.png"
$LogoPublicPath = Join-Path $PublicDir "nkb-logo.png"
$LogoAssetsPath = Join-Path $AssetsDir "nkb-logo.png"
$LogoDistPath = Join-Path $FrontendDistDir "nkb-logo.png"

$logoBmp.Save($LogoResPath, [System.Drawing.Imaging.ImageFormat]::Png)
$logoBmp.Save($LogoPublicPath, [System.Drawing.Imaging.ImageFormat]::Png)
$logoBmp.Save($LogoAssetsPath, [System.Drawing.Imaging.ImageFormat]::Png)
$logoBmp.Save($LogoDistPath, [System.Drawing.Imaging.ImageFormat]::Png)
$logoBmp.Dispose()
Write-Host "  + Saved nkb-logo.png to resources, public, and assets" -ForegroundColor Green

# 6. Generate High-Res Banner PNG (nkb-banner.png 1024x682)
Write-Host "[6/6] Generating branding banner PNG (1024x682)..." -ForegroundColor Yellow
$BannerResPath = Join-Path $ResDir "nkb-banner.png"
$BannerPublicPath = Join-Path $PublicDir "nkb-banner.png"
$BannerDistPath = Join-Path $FrontendDistDir "nkb-banner.png"

$bmp.Save($BannerResPath, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Save($BannerPublicPath, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Save($BannerDistPath, [System.Drawing.Imaging.ImageFormat]::Png)
Write-Host "  + Saved nkb-banner.png" -ForegroundColor Green

$bmp.Dispose()
Write-Host ""
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "   BRANDING ASSET GENERATION COMPLETE                           " -ForegroundColor Green
Write-Host "=================================================================" -ForegroundColor Cyan
