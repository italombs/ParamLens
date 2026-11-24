#!/bin/sh
# Joins the frames written by scripts/promo-video.js into dist/promo/paramlens-<lang>.mp4
# (1920x1080, 30 fps, H.264, 0.6 s crossfades). Needs ffmpeg.
set -e
for lang in en pt-BR; do
  dir="dist/promo/frames-$lang"
  inputs=""
  filter=""
  i=0
  offset=0
  prev="0:v"
  fade=0.6
  for d in $(cat "$dir/durations.txt"); do
    f=$(printf "%s/%02d.png" "$dir" $((i + 1)))
    inputs="$inputs -loop 1 -framerate 30 -t $d -i $f"
    if [ $i -gt 0 ]; then
      filter="$filter[$prev][$i:v]xfade=transition=fade:duration=$fade:offset=$offset[v$i];"
      prev="v$i"
    fi
    offset=$(awk -v o="$offset" -v d="$d" -v f="$fade" 'BEGIN { print o + d - f }')
    i=$((i + 1))
  done
  # shellcheck disable=SC2086
  ffmpeg -loglevel error -y $inputs -filter_complex "${filter}[$prev]format=yuv420p[out]" -map "[out]" \
    -r 30 -c:v libx264 -preset slow -crf 20 -movflags +faststart "dist/promo/paramlens-$lang.mp4"
  echo "wrote dist/promo/paramlens-$lang.mp4"
done
