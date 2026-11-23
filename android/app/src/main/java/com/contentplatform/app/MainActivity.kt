package com.contentplatform.app

import android.os.Bundle
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.core.view.WindowCompat
import com.contentplatform.app.ui.PlatformApp
import com.contentplatform.app.ui.theme.ContentPlatformTheme

/**
 * The single activity. It hardens the window before any content is drawn:
 * screenshots and screen recording are blocked with FLAG_SECURE, and the layout
 * is edge-to-edge with system bar colours owned by Compose.
 *
 * FLAG_SECURE is a deterrent, not a guarantee: a second physical camera can
 * still photograph the screen. The server-side watermark is the real control.
 */
class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE,
        )
        WindowCompat.setDecorFitsSystemWindows(window, false)
        enableEdgeToEdge()

        setContent {
            ContentPlatformTheme {
                PlatformApp()
            }
        }
    }
}
