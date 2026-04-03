package io.kombify.speechkit.app.ui

import android.app.Activity
import android.content.Intent
import android.os.Bundle

class DeepLinkActivity : Activity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val launchIntent = Intent(this, MainActivity::class.java).apply {
            action = intent?.action
            data = intent?.data
            addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        }
        intent?.extras?.let { launchIntent.putExtras(it) }

        startActivity(launchIntent)
        finish()
    }
}
