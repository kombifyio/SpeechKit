package io.kombify.speechkit.app.config

import android.content.Context
import android.content.SharedPreferences
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import timber.log.Timber
import java.nio.charset.StandardCharsets
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

interface HuggingFaceTokenStore {
    fun getToken(): String?
    fun saveToken(token: String)
    fun clearToken()
}

class AndroidKeystoreHuggingFaceTokenStore(
    private val context: Context,
) : HuggingFaceTokenStore {

    private val prefs: SharedPreferences
        get() = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    override fun getToken(): String? {
        val encodedIv = prefs.getString(KEY_IV, null) ?: return null
        val encodedCiphertext = prefs.getString(KEY_CIPHERTEXT, null) ?: return null

        return try {
            val cipher = Cipher.getInstance(TRANSFORMATION)
            val iv = Base64.decode(encodedIv, Base64.NO_WRAP)
            val ciphertext = Base64.decode(encodedCiphertext, Base64.NO_WRAP)
            cipher.init(Cipher.DECRYPT_MODE, getOrCreateSecretKey(), GCMParameterSpec(GCM_TAG_LENGTH_BITS, iv))
            val plaintext = cipher.doFinal(ciphertext).toString(StandardCharsets.UTF_8).trim()
            plaintext.takeIf { it.isNotEmpty() }
        } catch (e: Exception) {
            Timber.e(e, "Failed to decrypt HuggingFace token; clearing secure storage entry")
            clearToken()
            null
        }
    }

    override fun saveToken(token: String) {
        val normalized = token.trim()
        if (normalized.isEmpty()) {
            clearToken()
            return
        }

        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, getOrCreateSecretKey())
        val ciphertext = cipher.doFinal(normalized.toByteArray(StandardCharsets.UTF_8))

        prefs.edit()
            .putString(KEY_IV, Base64.encodeToString(cipher.iv, Base64.NO_WRAP))
            .putString(KEY_CIPHERTEXT, Base64.encodeToString(ciphertext, Base64.NO_WRAP))
            .apply()
    }

    override fun clearToken() {
        prefs.edit()
            .remove(KEY_IV)
            .remove(KEY_CIPHERTEXT)
            .apply()
    }

    private fun getOrCreateSecretKey(): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        val existingKey = keyStore.getKey(KEY_ALIAS, null) as? SecretKey
        if (existingKey != null) {
            return existingKey
        }

        val keyGenerator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        val spec = KeyGenParameterSpec.Builder(
            KEY_ALIAS,
            KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
        )
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setRandomizedEncryptionRequired(true)
            .build()
        keyGenerator.init(spec)
        return keyGenerator.generateKey()
    }

    private companion object {
        private const val PREFS_NAME = "speechkit_secure"
        private const val KEY_ALIAS = "speechkit_hf_token"
        private const val KEY_IV = "hf_token_iv"
        private const val KEY_CIPHERTEXT = "hf_token_ciphertext"
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val GCM_TAG_LENGTH_BITS = 128
    }
}
