package io.kombify.speechkit.net

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.io.File
import java.security.KeyStore
import java.util.UUID
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Key material is encrypted under a non-exportable device key and excluded from backup. */
internal class SecureProviderKeys(context: Context) {
    private val directory = File(context.noBackupFilesDir, "speechkit-provider-keys")
    private val alias = "speechkit-gemini-key-v1"

    private fun key(create: Boolean): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey(alias, null) as? SecretKey)?.let { return it }
        check(create) { "Stored credential is unavailable. Enter it again in Settings." }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .build())
        return generator.generateKey()
    }

    fun save(value: String): String {
        require(value.isNotBlank()) { "Enter a Gemini API key" }
        check(directory.isDirectory || directory.mkdirs()) { "Could not secure the Gemini key" }
        val id = UUID.randomUUID().toString()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key(true))
        val ciphertext = cipher.doFinal(value.trim().toByteArray(Charsets.UTF_8))
        File(directory, id).outputStream().use { out -> out.write(cipher.iv); out.write(ciphertext); out.fd.sync() }
        return id
    }

    fun load(id: String): String = try {
        require(UUID.fromString(id).toString() == id)
        val bytes = File(directory, id).readBytes()
        require(bytes.size > 28)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(false), GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
        cipher.doFinal(bytes.copyOfRange(12, bytes.size)).toString(Charsets.UTF_8).also { require(it.isNotBlank()) }
    } catch (error: Exception) {
        throw IllegalStateException("Stored credential is unavailable. Enter it again in Settings.", error)
    }

    fun delete(id: String?) {
        if (id != null && runCatching { UUID.fromString(id).toString() == id }.getOrDefault(false)) File(directory, id).delete()
    }
}
