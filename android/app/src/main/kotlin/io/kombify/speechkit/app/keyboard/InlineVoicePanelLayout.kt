package io.kombify.speechkit.app.keyboard

import android.graphics.Rect
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.ViewTreeObserver
import android.widget.FrameLayout
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

/** One composition moves between the native shortcut slot and the full keyboard area. */
internal class InlineVoicePanelLayout(
    private val keys: View,
    private val strip: View,
    private val suggestions: View,
    private val container: FrameLayout,
    private val content: View,
    private val onCompactAvailabilityChanged: (Boolean) -> Unit,
) {
    private val originalParams = FrameLayout.LayoutParams(container.layoutParams as FrameLayout.LayoutParams)
    private val stripVisibility = strip.visibility
    private val baseHeight = keys.height.coerceAtLeast(1)
    private val originalAlpha = keys.alpha
    private val originalAccessibility = keys.importantForAccessibility
    private val originalClickable = container.isClickable
    private val panelPadding = intArrayOf(container.paddingLeft, container.paddingTop, container.paddingRight, container.paddingBottom)
    private val suggestionAlpha = suggestions.alpha
    private val suggestionAccessibility = suggestions.importantForAccessibility
    private var compactAvailable: Boolean? = null
    private var compact = false
    private var bounded = false
    private var closed = false
    private var initialized = false
    private val listener = ViewTreeObserver.OnGlobalLayoutListener { positionPanel() }

    init {
        keys.viewTreeObserver.addOnGlobalLayoutListener(listener)
        container.isClickable = true
    }

    fun show(compact: Boolean, bounded: Boolean) {
        if (closed || (initialized && this.compact == compact && this.bounded == bounded)) return
        initialized = true
        this.compact = compact
        this.bounded = bounded
        // LatinIME still measures the real frame for its touchable region.
        keys.visibility = if (compact || bounded) View.VISIBLE else View.INVISIBLE
        keys.alpha = if (!compact && bounded) 0f else originalAlpha
        keys.importantForAccessibility = if (!compact && bounded) View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS else originalAccessibility
        positionPanel()
        content.requestLayout()
    }

    private fun positionPanel() {
        if (closed) return
        val available = suggestions.visibility == View.VISIBLE && suggestions.width > 0
        if (compactAvailable != available) {
            compactAvailable = available
            onCompactAvailabilityChanged(available)
        }
        // Toolbar-only mode has no free center slot. Let the host expand;
        // never cover or remove that mode's native shortcut scroller.
        if (compact && !available) {
            container.visibility = View.GONE
            return
        }
        container.visibility = View.VISIBLE
        strip.visibility = if (compact) View.VISIBLE else stripVisibility
        suggestions.alpha = if (compact) 0f else suggestionAlpha
        suggestions.importantForAccessibility = if (compact) View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS else suggestionAccessibility
        val navigation = if (!compact && bounded) ViewCompat.getRootWindowInsets(container)
            ?.getInsets(WindowInsetsCompat.Type.navigationBars())?.bottom ?: 0 else 0
        val bottom = panelPadding[3] + navigation
        if (container.paddingBottom != bottom) container.setPadding(panelPadding[0], panelPadding[1], panelPadding[2], bottom)
        val next = FrameLayout.LayoutParams(originalParams)
        if (compact) {
            // Overlay only the center slot. More and pinned shortcuts remain native,
            // and neither the strip nor any letter key moves when a session opens.
            val bounds = Rect(0, 0, suggestions.width, suggestions.height)
            (container.parent as ViewGroup).offsetDescendantRectToMyCoords(suggestions, bounds)
            next.gravity = Gravity.TOP or Gravity.LEFT
            next.leftMargin = bounds.left
            next.topMargin = bounds.top
            next.width = bounds.width().coerceAtLeast(1)
            next.height = bounds.height().coerceAtLeast(1)
        } else {
            next.gravity = Gravity.BOTTOM
            next.height = if (bounded) baseHeight else ViewGroup.LayoutParams.WRAP_CONTENT
        }
        val current = container.layoutParams as FrameLayout.LayoutParams
        if (current.width != next.width || current.height != next.height || current.gravity != next.gravity ||
            current.leftMargin != next.leftMargin || current.topMargin != next.topMargin) container.layoutParams = next
    }

    fun close() {
        closed = true
        if (keys.viewTreeObserver.isAlive) keys.viewTreeObserver.removeOnGlobalLayoutListener(listener)
        strip.visibility = stripVisibility
        suggestions.alpha = suggestionAlpha
        suggestions.importantForAccessibility = suggestionAccessibility
        keys.visibility = View.VISIBLE
        keys.alpha = originalAlpha
        keys.importantForAccessibility = originalAccessibility
        container.isClickable = originalClickable
        container.setPadding(panelPadding[0], panelPadding[1], panelPadding[2], panelPadding[3])
        container.layoutParams = originalParams
    }
}
