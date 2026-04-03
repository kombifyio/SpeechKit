package io.kombify.speechkit.app.stt

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import dagger.hilt.android.qualifiers.ApplicationContext
import io.kombify.speechkit.app.config.HuggingFaceTokenStore
import io.kombify.speechkit.stt.HuggingFaceProvider
import io.kombify.speechkit.stt.SttRouter
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class AndroidSttRouterConfigurator @Inject constructor(
    @ApplicationContext private val context: Context,
    private val tokenStore: HuggingFaceTokenStore,
) {

    fun createRouter(strategy: SttRouter.RoutingStrategy): SttRouter {
        val router = SttRouter(
            strategy = strategy,
            preferLocalUnderSecs = 10.0,
            parallelCloud = false,
            connectivityCheck = { isOnline() },
        )
        refreshCloudProviders(router)
        return router
    }

    fun refreshCloudProviders(router: SttRouter) {
        val token = tokenStore.getToken()
        router.setCloud(
            HuggingFaceProviderName,
            token?.takeIf { it.isNotBlank() }?.let { HuggingFaceProvider(token = it) },
        )
    }

    private fun isOnline(): Boolean {
        val connectivityManager = context.getSystemService(ConnectivityManager::class.java) ?: return false
        val activeNetwork = connectivityManager.activeNetwork ?: return false
        val capabilities = connectivityManager.getNetworkCapabilities(activeNetwork) ?: return false
        return capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
    }

    companion object {
        const val HuggingFaceProviderName = "huggingface"
    }
}
