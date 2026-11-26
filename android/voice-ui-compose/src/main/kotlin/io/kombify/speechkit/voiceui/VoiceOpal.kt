package io.kombify.speechkit.voiceui

import android.graphics.Bitmap
import android.graphics.BlurMaskFilter
import android.graphics.Canvas as NativeCanvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.Shader
import android.database.ContentObserver
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.view.View
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlin.math.*

/** Core-to-canvas ratio of the approved Opal artwork. */
enum class OpalProfile { ORIGINAL, SMALL }

@Composable
private fun opalMotionSuppressed(): Boolean {
    val resolver = LocalContext.current.contentResolver
    fun disabled() = Settings.Global.getFloat(resolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
    var suppressed by remember(resolver) { mutableStateOf(disabled()) }
    DisposableEffect(resolver) {
        val observer = object : ContentObserver(Handler(Looper.getMainLooper())) {
            override fun onChange(selfChange: Boolean) { suppressed = disabled() }
        }
        resolver.registerContentObserver(Settings.Global.getUriFor(Settings.Global.ANIMATOR_DURATION_SCALE), false, observer)
        onDispose { resolver.unregisterContentObserver(observer) }
    }
    return suppressed
}

/** Native Opal; [visible] must reflect the host surface's visibility. Levels are real 0..1 amplitudes. */
@Composable
fun VoiceOpal(
    state: VoiceAuraState,
    modifier: Modifier = Modifier,
    inputLevel: Float = 0f,
    outputLevel: Float = 0f,
    sizeDp: Int = 96,
    reduceMotion: Boolean = opalMotionSuppressed(),
    profile: OpalProfile = OpalProfile.ORIGINAL,
    visible: Boolean = true,
) {
    val view = LocalView.current
    val renderer = remember { NativeOpal() }
    val tick = remember { mutableLongStateOf(0L) }
    // PCM can arrive faster than the animation clock. Keep it out of snapshot
    // draw dependencies so audio updates cannot drive extra raster frames.
    SideEffect { renderer.inputLevel = inputLevel; renderer.outputLevel = outputLevel }
    val animate = visible && !reduceMotion && state != VoiceAuraState.INACTIVE && state != VoiceAuraState.ERROR
    LaunchedEffect(animate, view) {
        if (animate) while (true) {
            if (view.isShown && view.windowVisibility == View.VISIBLE) tick.longValue = android.os.SystemClock.uptimeMillis()
            delay(if (view.isShown && view.windowVisibility == View.VISIBLE) 40 else 500)
        }
    }
    val draw: DrawScope.() -> Unit = remember(renderer, state, profile, reduceMotion, visible) {
        {
            if (visible) drawIntoCanvas { canvas ->
                renderer.draw(canvas.nativeCanvas, size.width, size.height, state, renderer.inputLevel, renderer.outputLevel, profile,
                    reduceMotion || state == VoiceAuraState.INACTIVE || state == VoiceAuraState.ERROR, tick.longValue)
            }
        }
    }
    Canvas(modifier = modifier.size(sizeDp.dp), onDraw = draw)
}

private class NativeOpal {
    var inputLevel: Float = 0f
    var outputLevel: Float = 0f
    private data class Glass(val index: Int, val sky: Double, val base: Double, val edge: Double, val alpha: Int)
    private data class Haze(val index: Int, val d: Double, val x: Double, val y: Double, val c2: Double, val s2: Double, val c3: Double, val s3: Double)
    private val glass = Bitmap.createBitmap(192, 192, Bitmap.Config.ARGB_8888)
    private val mist = Bitmap.createBitmap(96, 96, Bitmap.Config.ARGB_8888)
    private val glassPixels = IntArray(192 * 192)
    private val mistPixels = IntArray(96 * 96)
    private val geometry = ArrayList<Glass>()
    private val haze = ArrayList<Haze>()
    private val inputHistory = DoubleArray(96)
    private val outputHistory = DoubleArray(96)
    private val weights = doubleArrayOf(1.0, 0.0, 0.0)
    private val velocity = DoubleArray(3)
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG or Paint.FILTER_BITMAP_FLAG)
    private val ribbonPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE; strokeCap = Paint.Cap.ROUND; strokeJoin = Paint.Join.ROUND }
    private var currentProfile: OpalProfile? = null
    private var radius = 55.5
    private var reach = 39.5
    private var index = 0
    private var last = 0L
    private var time = 0.0
    private var errorTint = 0.0
    private var activity = 1.0

    init {
        for (row in 0 until 192) for (col in 0 until 192) {
            val x = (2.0 * col + 1 - 192) / 190.5
            val y = (2.0 * row + 1 - 192) / 190.5
            val r = hypot(x, y)
            if (r >= 1) continue
            val z = sqrt(1 - r * r)
            val u = x * .937 - y * .35
            val sky = clamp(.54 - .29 * x - .49 * y + .16 * z)
            val spec = max(0.0, -.53 * x - .63 * y + .57 * z).pow(29)
            val edge = (1 - z).pow(3)
            val rim = 1 / (1 + 1800 * (z - .18).pow(2))
            val shimmer = clamp(1 - z * 2.2) * (.5 + .5 * cos(56 * z + u * 5))
            geometry.add(Glass(row * 192 + col, sky, 17 + 71 * sky + 66 * sky * sky + 76 * edge + 47 * rim + 87 * spec + 16 * shimmer, edge, (clamp((1 - r) * 96) * 255).roundToInt()))
        }
    }

    private fun setProfile(profile: OpalProfile) {
        if (profile == currentProfile) return
        currentProfile = profile
        radius = if (profile == OpalProfile.SMALL) 32.0 else 55.5
        reach = 95 - radius
        haze.clear(); mistPixels.fill(0)
        for (y in 0 until 96) for (x in 0 until 96) {
            val dx = (x + .5) * 2 - 96; val dy = (y + .5) * 2 - 96
            val d = hypot(dx, dy) - radius
            if (d < -2 || d > reach) continue
            val a = atan2(dy, dx)
            haze.add(Haze(y * 96 + x, max(0.0, d), cos(a), sin(a), cos(2*a), sin(2*a), cos(3*a), sin(3*a)))
        }
    }

    fun draw(canvas: NativeCanvas, width: Float, height: Float, state: VoiceAuraState, inputLevel: Float, outputLevel: Float, profile: OpalProfile, reduced: Boolean, tick: Long) {
        setProfile(profile)
        val dt = if (last == 0L) .04 else ((tick - last) / 1000.0).coerceIn(0.0, .1)
        last = tick
        val input = if (!reduced && inputLevel.isFinite()) clamp(inputLevel.toDouble()) else 0.0
        val output = if (!reduced && outputLevel.isFinite()) clamp(outputLevel.toDouble()) else 0.0
        val overlap = if (state == VoiceAuraState.SPEAKING) clamp(input * 1.5) * .72 else 0.0
        val target = when (state) {
            VoiceAuraState.CONNECTING, VoiceAuraState.PROCESSING, VoiceAuraState.RECOVERING -> doubleArrayOf(0.0, 1.0, 0.0)
            VoiceAuraState.SPEAKING -> doubleArrayOf(overlap, 0.0, 1 - overlap)
            else -> doubleArrayOf(1.0, 0.0, 0.0)
        }
        val rate = 6.6; val decay = exp(-rate * dt)
        for (i in 0..2) {
            val delta = weights[i] - target[i]; val slope = velocity[i] + rate * delta
            weights[i] = if (reduced) target[i] else target[i] + (delta + slope * dt) * decay
            velocity[i] = if (reduced) 0.0 else (velocity[i] - rate * slope * dt) * decay
        }
        val error = if (state == VoiceAuraState.ERROR) 1.0 else 0.0
        val active = if (state == VoiceAuraState.INACTIVE) .45 else 1.0
        errorTint = if (reduced) error else errorTint + (error - errorTint) * (1 - decay)
        activity = if (reduced) active else activity + (active - activity) * (1 - decay)
        if (!reduced && tick != 0L) time += dt
        if (reduced) {
            inputHistory.fill(0.0)
            outputHistory.fill(0.0)
        } else index = (index + 1) % 96
        inputHistory[index] = if (reduced) 0.0 else input
        outputHistory[index] = if (reduced) 0.0 else output
        val l = weights[0]; val t = weights[1]; val s = weights[2]
        val rgb = oklch(.75*l + .70*t + .78*s, .13*l + .18*(t+s), (210*l + 285*t + 405*s)*(1-errorTint) + 25*errorTint)
        for (p in geometry) {
            val base = .78*p.base; val dye = .76*(.17 + .36*p.sky)
            glassPixels[p.index] = argb(p.alpha.toDouble(), base + dye*rgb[0], base + dye*rgb[1] + 3 + 6*p.edge, base + dye*rgb[2] + 8*p.sky)
        }
        glass.setPixels(glassPixels, 0, 192, 0, 0, 192, 192)
        drawRibbon(NativeCanvas(glass), if (reduced) 0.0 else time, l, t, s, if (reduced) 0.0 else input, if (reduced) 0.0 else output, rgb)
        val save = canvas.save(); canvas.scale(width / 192f, height / 192f)
        drawMist(canvas, if (reduced) 0.0 else time, s, false, rgb)
        drawMist(canvas, if (reduced) 0.0 else time, l, true, rgb)
        paint.alpha = (255 * activity).roundToInt().coerceIn(0,255)
        canvas.drawBitmap(glass, null, android.graphics.RectF((96-radius).toFloat(), (96-radius).toFloat(), (96+radius).toFloat(), (96+radius).toFloat()), paint)
        canvas.restoreToCount(save)
    }
    private fun drawMist(canvas: NativeCanvas, time: Double, strength: Double, listening: Boolean, rgb: DoubleArray) {
        if (strength < .002) return
        val drift = time * .58; val ca = cos(drift); val sa = sin(drift); val cb = cos(.83*drift); val sb = sin(.83*drift)
        val history = if (listening) inputHistory else outputHistory
        for (p in haze) {
            val delay = p.d * if (listening) .05 else .22
            val newer = (index - floor(delay).toInt() + 96) % 96
            val from = history[newer]; val to = history[(newer + 95) % 96]
            val level = from + (to-from) * (delay % 1)
            val billow = .55 + .21*(p.c2*ca+p.s2*sa) + .13*(p.c3*cb-p.s3*sb) - .08*p.y + .03*p.x
            val spread = (6.5 + (11.5 + 23*level)*billow + 9.5*level) * if (currentProfile == OpalProfile.SMALL) 1.65 else 1.0
            val density = exp(-1.85*(p.d/spread).pow(2)); val edge = clamp((reach-p.d)/8)
            val pearl = exp(-p.d/4.5)*(.22 + .25*level)
            val intensity = if (listening) .86*sqrt(level) else .16 + .61*level
            mistPixels[p.index] = argb(255*strength*edge*edge*density*intensity*(.74+.38*billow)*activity,
                rgb[0]+(255-rgb[0])*pearl, rgb[1]+(244-rgb[1])*pearl, rgb[2]+(213-rgb[2])*pearl)
        }
        mist.setPixels(mistPixels, 0, 96, 0, 0, 96, 96)
        paint.alpha = 255
        canvas.drawBitmap(mist, null, android.graphics.RectF(0f,0f,192f,192f), paint)
    }

    private fun drawRibbon(canvas: NativeCanvas, time: Double, l: Double, t: Double, s: Double, input: Double, voice: Double, rgb: DoubleArray) {
        val line = Path(); val fold = Path(); val depth = 192*(.018+.034*l+.064*s)
        fun point(f: Double): Pair<Float, Float> {
            val angle = (2*f-1)*PI*(1+t)/2
            val x = (1.09-.39*t)*sin(angle)
            val bowl = .34-.82*x*x-.15*input*(1-x*x)+.022*sin(1.7*time)
            val wave = -.22*x+.24*sin(3.75*x+.42*sin(2.6*time))+.11-.21*voice
            val y = l*bowl+t*.32*cos(angle)+s*wave; val tilt = -.36*t-.38*s
            return (192*(.5+.499*(x*cos(tilt)-y*sin(tilt)))).toFloat() to (192*(.5+.499*(x*sin(tilt)+y*cos(tilt)))).toFloat()
        }
        for (k in 0..160) {
            val f = k/160.0; val (x,y) = point(f)
            val offset = (depth*(.71+.29*cos(2*PI*(f-.5)))).toFloat()
            if (k == 0) { line.moveTo(x,y); fold.moveTo(x,y+offset) } else { line.lineTo(x,y); fold.lineTo(x,y+offset) }
        }
        val save = canvas.save()
        val clip = Path().apply { addCircle(96f,96f,95.25f,Path.Direction.CW) }
        canvas.clipPath(clip)
        fun stroke(path: Path, width: Float, color: Int, blur: Float) {
            ribbonPaint.strokeWidth = width; ribbonPaint.color = color
            ribbonPaint.maskFilter = BlurMaskFilter(blur, BlurMaskFilter.Blur.NORMAL)
            canvas.drawPath(path, ribbonPaint)
        }
        stroke(fold,12f,argb(76.5,12.0,16.0,35.0),4.5f)
        stroke(line,14f,argb(229.5,rgb[0],rgb[1],rgb[2]),6.5f)
        stroke(fold,2.5f,argb(255*(.18+.14*s),247.0,245.0,245.0),1.5f)
        stroke(line,6f,argb(130.0,255.0,248.0,246.0),2.5f)
        stroke(line,1.35f,argb(237.0,255.0,253.0,249.0),.7f)
        val f = (time*.22+.21)%1; val (x,y) = point(f)
        val seam = clamp(min(f,1-f)*13); val highlight = t*t*(t+(1-t)*seam)
        if (highlight > .002) {
            val glow = Paint(Paint.ANTI_ALIAS_FLAG).apply {
                shader = RadialGradient(x,y,7f,intArrayOf(argb(255*highlight,255.0,255.0,255.0),argb(255*highlight*.85,255.0,255.0,255.0),0),floatArrayOf(0f,.24f,1f),Shader.TileMode.CLAMP)
            }
            canvas.drawCircle(x,y,7f,glow)
        }
        canvas.restoreToCount(save)
    }
}

private fun clamp(v: Double): Double = v.coerceIn(0.0,1.0)
private fun argb(a: Double, r: Double, g: Double, b: Double): Int = android.graphics.Color.argb(a.roundToInt().coerceIn(0,255),r.roundToInt().coerceIn(0,255),g.roundToInt().coerceIn(0,255),b.roundToInt().coerceIn(0,255))

// OKLCH -> OKLab -> linear sRGB -> sRGB, matching CSS canvas colour conversion.
private fun oklch(l: Double, c: Double, hue: Double): DoubleArray {
    val radians = hue*PI/180; val a = c*cos(radians); val b = c*sin(radians)
    val ll = (l + .3963377774*a + .2158037573*b).pow(3)
    val mm = (l - .1055613458*a - .0638541728*b).pow(3)
    val ss = (l - .0894841775*a - 1.291485548*b).pow(3)
    fun srgb(x: Double): Double = 255*clamp(if (x <= .0031308) 12.92*x else 1.055*x.pow(1/2.4)-.055)
    return doubleArrayOf(srgb(4.0767416621*ll-3.3077115913*mm+.2309699292*ss), srgb(-1.2684380046*ll+2.6097574011*mm-.3413193965*ss), srgb(-.0041960863*ll-.7034186147*mm+1.7076147010*ss))
}
