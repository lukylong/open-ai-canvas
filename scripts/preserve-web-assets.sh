#!/bin/sh
set -eu

source_dir="${CANVAS_STATIC_SOURCE:-/usr/share/nginx/html/assets}"
target_dir="${CANVAS_STATIC_ROOT:-/var/lib/canvas-static}/assets"

# 内容哈希文件必须跨发布保留，旧标签页仍可能按旧入口请求尚未加载的模块。
# 不覆盖、不删除旧文件；HTML 始终来自当前镜像，不进入此共享目录。
test -d "$source_dir"
mkdir -p "$target_dir"
find "${source_dir%/}" -type f -exec sh -eu -c '
    source_dir=$1
    target_dir=$2
    shift 2
    for file do
        relative=${file#"$source_dir/"}
        destination="$target_dir/$relative"
        if [ -f "$destination" ]; then continue; fi
        mkdir -p "$(dirname "$destination")"
        temporary=$(mktemp "$destination.tmp.XXXXXX")
        trap '\''rm -f "$temporary"'\'' EXIT
        cp "$file" "$temporary"
        chmod 644 "$temporary"
        # 完整写入后原子发布；并发启动时已有相同哈希文件则保留先到者。
        if ! ln "$temporary" "$destination" 2>/dev/null && [ ! -f "$destination" ]; then
            exit 1
        fi
        rm -f "$temporary"
        trap - EXIT
    done
' sh "${source_dir%/}" "$target_dir" {} +
