# Maintainer: mcbalaam <your@email>
pkgname=graft
pkgver=0.1.0
pkgrel=1
pkgdesc="CLI tool for tracking arbitrary directories via git submodules"
arch=('x86_64' 'aarch64')
url="https://github.com/mcbalaam/graft"
license=('MIT')
depends=('git')
makedepends=('go')
source=("$pkgname-$pkgver.tar.gz::$url/archive/v$pkgver.tar.gz")
sha256sums=('SKIP')

build() {
    cd "$pkgname-$pkgver"
    make build
}

package() {
    cd "$pkgname-$pkgver"
    make install DESTDIR="$pkgdir" PREFIX=/usr
    install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
