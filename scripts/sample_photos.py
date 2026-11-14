#!/usr/bin/env python3
"""Draws a few landscapes to stand in for someone's photos.

The screenshots on the project page need pictures behind the clock, and
nobody's holiday photos belong in this repository. These are drawn from
scratch: a sky, a sun or moon, and ridges of hills fading into the distance.

Usage: sample_photos.py OUTPUT_DIR
Needs numpy and Pillow.
"""
import os
import sys
import time

import numpy as np
from PIL import Image

W, H = 2560, 1440


def rgb(h):
    return np.array([int(h[i : i + 2], 16) for i in (1, 3, 5)], dtype=np.float32) / 255


def ridge(rng, rough, octaves=6):
    """A skyline: one height per column, 0..1, made of ever finer bumps."""
    x = np.linspace(0, 1, W)
    y = np.zeros(W)
    amp, freq = 1.0, rng.uniform(1.2, 2.2)
    for _ in range(octaves):
        pts = rng.uniform(-1, 1, int(freq) + 3)
        y += amp * np.interp(x * freq, np.arange(len(pts)), pts)
        amp *= rough
        freq *= 2.1
    y -= y.min()
    return y / y.max()


def paint(seed, sky, sun, near, far, layers=6, stars=False, water=False, sun_at=(0.68, 0.36)):
    rng = np.random.default_rng(seed)
    yy, xx = np.mgrid[0:H, 0:W].astype(np.float32)
    v = yy / H

    # Sky: three colours from the top to the horizon.
    top, mid, low = (rgb(c) for c in sky)
    t = np.clip(v / 0.72, 0, 1)[..., None]
    img = np.where(t < 0.55, top + (mid - top) * (t / 0.55), mid + (low - mid) * ((t - 0.55) / 0.45))

    if stars:
        n = 900
        sx, sy = rng.integers(0, W, n), (rng.random(n) ** 1.6 * H * 0.6).astype(int)
        img[sy, sx] += rng.uniform(0.25, 0.9, (n, 1))

    # The sun or moon: a small disc in a wide glow.
    cx, cy = sun_at[0] * W, sun_at[1] * H
    d = np.hypot(xx - cx, (yy - cy) * 1.05) / H
    glow = rgb(sun)
    img += glow * (0.55 * np.exp(-d * 5.5) + 0.35 * np.exp(-d * 18))[..., None]
    img = np.where((d < 0.022)[..., None], np.maximum(img, glow * 1.05 + 0.12), img)

    # Hills, from the farthest to the nearest. Distance fades them into the
    # colour of the horizon, the way haze does.
    c_near, c_far = rgb(near), rgb(far)
    horizon = 0.80 if water else 1.0
    for i in range(layers):
        k = i / (layers - 1)
        base = (0.44 + 0.44 * k) * horizon + 0.02
        height = (0.22 - 0.10 * k) * horizon
        line = (base - ridge(rng, 0.52 + 0.06 * k) * height) * H
        colour = c_far + (c_near - c_far) * (k**1.25)
        inside = yy >= line[None, :]
        # A little lighter along each crest, darker towards its foot.
        depth = np.clip((yy - line[None, :]) / (H * 0.3), 0, 1)[..., None]
        img = np.where(inside[..., None], colour * (1.08 - 0.22 * depth), img)

    if water:
        # A lake: what is above the shore, upside down, darker and a little restless.
        shore = int(H * horizon)
        rows = np.arange(shore, H)
        src = np.clip(shore - 1 - ((rows - shore) * 1.25).astype(int), 0, shore - 1)
        ripple = (np.sin(rows * 0.9)[:, None] * 5 * ((rows - shore) / (H - shore))[:, None]).astype(int)
        cols = np.clip(np.arange(W)[None, :] + ripple, 0, W - 1)
        img[shore:] = img[src[:, None], cols] * 0.72 + rgb(sky[0]) * 0.10

    # Grain, so that the smooth sky does not break into bands as a JPEG.
    img += rng.normal(0, 0.006, img.shape[:2])[..., None]
    return Image.fromarray((np.clip(img, 0, 1) * 255).astype(np.uint8))


PICTURES = [
    # album, file, date, settings
    ("Alps", "first-light.jpg", "2025-08-14", dict(seed=11, sky=("#3b5b92", "#f2a98a", "#ffd9a8"), sun="#fff0c8", near="#1d2a44", far="#c79aa4", sun_at=(0.70, 0.40))),
    ("Alps", "blue-hour.jpg", "2025-08-15", dict(seed=5, sky=("#0c1836", "#27407a", "#6f86b8"), sun="#e8eeff", near="#070d1c", far="#5b74a8", stars=True, sun_at=(0.24, 0.20))),
    ("Lake", "evening.jpg", "2025-06-21", dict(seed=23, sky=("#2a2350", "#c8557a", "#ffb36b"), sun="#ffe2a6", near="#15122b", far="#b0607e", water=True, sun_at=(0.42, 0.44))),
    ("Lake", "midday.jpg", "2025-06-22", dict(seed=31, sky=("#2f7fd0", "#8cc4ee", "#dff1fb"), sun="#ffffff", near="#12402f", far="#9cc3cf", water=True, sun_at=(0.80, 0.16))),
    ("Autumn", "ridges.jpg", "2024-10-05", dict(seed=47, sky=("#5d7fa8", "#f0c08a", "#ffe6b8"), sun="#fff3cf", near="#3a1c12", far="#d8a97a", sun_at=(0.30, 0.38))),
]


def main():
    out = sys.argv[1]
    for album, name, date, settings in PICTURES:
        os.makedirs(os.path.join(out, album), exist_ok=True)
        path = os.path.join(out, album, name)
        paint(**settings).save(path, quality=90)
        # The slideshow shows when a photo was taken; a file's date stands in for that.
        when = time.mktime(time.strptime(date + " 12:00", "%Y-%m-%d %H:%M"))
        os.utime(path, (when, when))
        print("drew", path)


if __name__ == "__main__":
    main()
