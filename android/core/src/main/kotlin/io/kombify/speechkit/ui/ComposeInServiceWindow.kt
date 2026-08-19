package io.kombify.speechkit.ui

import android.view.View
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.LifecycleRegistry
import androidx.lifecycle.ViewModelStore
import androidx.lifecycle.ViewModelStoreOwner
import androidx.lifecycle.setViewTreeLifecycleOwner
import androidx.lifecycle.setViewTreeViewModelStoreOwner
import androidx.savedstate.SavedStateRegistry
import androidx.savedstate.SavedStateRegistryController
import androidx.savedstate.SavedStateRegistryOwner
import androidx.savedstate.setViewTreeSavedStateRegistryOwner

/**
 * Manual view-tree owners for a View hosted in a Service-owned window.
 *
 * Compose (and Views that use lifecycle-aware components) resolves its
 * lifecycle, ViewModel store, and saved-state registry from the view tree.
 * In an Activity those owners are attached automatically; in a Service
 * window — an IME input view, an assistant overlay — nobody does it, and
 * `ComposeView.setContent` throws "ViewTreeLifecycleOwner not found".
 *
 * Usage (e.g. from `InputMethodService.onCreateInputView`):
 * ```
 * val owner = ServiceWindowOwner()          // starts in CREATED
 * owner.attachTo(composeView)
 * composeView.setContent { ... }
 * // onStartInputView  -> owner.onResume()
 * // onFinishInputView -> owner.onPause()
 * // onDestroy         -> owner.onDestroy()
 * ```
 *
 * One owner per hosted view: recreate it together with the view. The owner is
 * not restartable after [onDestroy].
 */
class ServiceWindowOwner : LifecycleOwner, ViewModelStoreOwner, SavedStateRegistryOwner {

    private val lifecycleRegistry = LifecycleRegistry(this)
    private val store = ViewModelStore()
    private val savedStateController = SavedStateRegistryController.create(this)

    override val lifecycle: Lifecycle get() = lifecycleRegistry
    override val viewModelStore: ViewModelStore get() = store
    override val savedStateRegistry: SavedStateRegistry
        get() = savedStateController.savedStateRegistry

    init {
        // A service window has no saved instance state to restore; the
        // registry still has to be attached and restored (with null) before
        // any SavedStateRegistry consumer touches it.
        savedStateController.performAttach()
        savedStateController.performRestore(null)
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_CREATE)
    }

    /** Installs this owner on [view]'s view tree. Call before `setContent`. */
    fun attachTo(view: View) {
        view.setViewTreeLifecycleOwner(this)
        view.setViewTreeViewModelStoreOwner(this)
        view.setViewTreeSavedStateRegistryOwner(this)
    }

    /** The hosted window became visible/interactive (e.g. onStartInputView). */
    fun onResume() {
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_RESUME)
    }

    /** The hosted window was hidden (e.g. onFinishInputView). */
    fun onPause() {
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_PAUSE)
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_STOP)
    }

    /** Terminal: tears the lifecycle down and clears the ViewModel store. */
    fun onDestroy() {
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_DESTROY)
        store.clear()
    }
}
