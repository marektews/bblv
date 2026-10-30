import * as THREE from 'three'
import { OrbitControls } from 'three/addons/controls/OrbitControls.js'
import { featureColor } from './features'
import { POINT_FLOATS, type LoadedGeometry } from './geometry'

export type ColorMode = 'filament' | 'feature'

const MAX_TOOLS = 16
const MAX_FEATURES = 64
// Above these segment counts the solid view is too heavy and thin lines are drawn instead.
const SOLID_LIMIT_DESKTOP = 2_500_000
const SOLID_LIMIT_MOBILE = 400_000

// Instance i is the segment from point i to point i+1 and uses the attributes
// of point i+1; both ends are read from the same buffer, one point apart.
const commonVertex = /* glsl */ `
  attribute vec3 iStart;
  attribute vec3 iEnd;
  attribute vec4 iProps; // width, height, layer, tool*256+feature
  uniform float uLayer;  // layers below are printed, above are the ghost
  uniform float uGhost;  // 1 when drawing the ghost pass
  uniform float uMode;   // 0 = filament colours, 1 = feature colours
  uniform vec3 uTools[${MAX_TOOLS}];
  uniform vec3 uFeatures[${MAX_FEATURES}];
  varying vec3 vColor;

  bool hidden() {
    bool ghost = iProps.z > uLayer + 0.5;
    return iProps.x <= 0.0 || ghost != (uGhost > 0.5);
  }

  vec3 segmentColor() {
    float tool = floor(iProps.w / 256.0);
    float feature = iProps.w - tool * 256.0;
    vec3 c = vec3(0.5);
    if (uMode > 0.5) {
      if (feature < ${MAX_FEATURES}.0) c = uFeatures[int(feature)];
    } else if (tool < ${MAX_TOOLS}.0) {
      c = uTools[int(tool)];
    }
    // Highlight the layer being printed.
    if (abs(iProps.z - uLayer) < 0.5) c = mix(c, vec3(0.0, 0.85, 1.0), 0.45);
    return c;
  }
`

const solidVertex = /* glsl */ `
  ${commonVertex}
  varying vec3 vNormal;

  void main() {
    if (hidden()) {
      gl_Position = vec4(2.0, 2.0, 2.0, 1.0); // outside the clip volume
      vColor = vec3(0.0);
      vNormal = vec3(0.0, 0.0, 1.0);
      return;
    }
    vec3 dir = iEnd - iStart;
    float len = length(dir);
    vec3 fwd = len > 1e-6 ? dir / len : vec3(1.0, 0.0, 0.0);
    vec3 side = cross(vec3(0.0, 0.0, 1.0), fwd);
    side = length(side) > 1e-6 ? normalize(side) : vec3(1.0, 0.0, 0.0);
    vec3 up = cross(fwd, side);
    float width = iProps.x;
    float ext = width * 0.5; // overlap at joints so corners have no gaps
    // position: x along the segment 0..1, y across -0.5..0.5, z -0.5..0.5 below the nozzle height
    vec3 p = iStart + fwd * (position.x * (len + 2.0 * ext) - ext)
      + side * (position.y * width) + up * ((position.z - 0.5) * iProps.y);
    vNormal = normalize(normalMatrix * (fwd * normal.x + side * normal.y + up * normal.z));
    vColor = segmentColor();
    gl_Position = projectionMatrix * modelViewMatrix * vec4(p, 1.0);
  }
`

const solidFragment = /* glsl */ `
  uniform float uGhost;
  uniform float uOpacity;
  varying vec3 vColor;
  varying vec3 vNormal;

  void main() {
    vec3 n = normalize(vNormal);
    float light = 0.35 + 0.65 * max(dot(n, normalize(vec3(0.3, 0.5, 1.0))), 0.0);
    vec3 c = uGhost > 0.5 ? vec3(0.7) : vColor;
    gl_FragColor = vec4(c * light, uGhost > 0.5 ? uOpacity : 1.0);
    #include <colorspace_fragment>
  }
`

const lineVertex = /* glsl */ `
  ${commonVertex}

  void main() {
    if (hidden()) {
      gl_Position = vec4(2.0, 2.0, 2.0, 1.0);
      vColor = vec3(0.0);
      return;
    }
    vColor = segmentColor();
    gl_Position = projectionMatrix * modelViewMatrix * vec4(mix(iStart, iEnd, position.x), 1.0);
  }
`

const lineFragment = /* glsl */ `
  uniform float uGhost;
  uniform float uOpacity;
  varying vec3 vColor;

  void main() {
    gl_FragColor = vec4(uGhost > 0.5 ? vec3(0.7) : vColor, uGhost > 0.5 ? uOpacity : 1.0);
    #include <colorspace_fragment>
  }
`

// prism builds a unit segment with a hexagonal cross-section and flat normals.
function prism(): THREE.BufferGeometry {
  const ring: [number, number][] = [
    [-0.5, 0],
    [-0.25, 0.5],
    [0.25, 0.5],
    [0.5, 0],
    [0.25, -0.5],
    [-0.25, -0.5],
  ]
  const pos: number[] = []
  const nor: number[] = []
  const idx: number[] = []
  for (let k = 0; k < ring.length; k++) {
    const [y0, z0] = ring[k]
    const [y1, z1] = ring[(k + 1) % ring.length]
    const n = new THREE.Vector2(z1 - z0, -(y1 - y0)).normalize()
    if (n.x * (y0 + y1) + n.y * (z0 + z1) < 0) {
      n.negate() // point outwards
    }
    const b = pos.length / 3
    for (const [x, y, z] of [
      [0, y0, z0],
      [1, y0, z0],
      [1, y1, z1],
      [0, y1, z1],
    ]) {
      pos.push(x, y, z)
      nor.push(0, n.x, n.y)
    }
    idx.push(b, b + 1, b + 2, b, b + 2, b + 3)
  }
  for (const x of [0, 1]) {
    const b = pos.length / 3
    for (const [y, z] of ring) {
      pos.push(x, y, z)
      nor.push(x === 0 ? -1 : 1, 0, 0)
    }
    // The caps face -x and +x, so they need opposite windings.
    for (let k = 1; k < ring.length - 1; k++) {
      idx.push(...(x === 0 ? [b, b + k, b + k + 1] : [b, b + k + 1, b + k]))
    }
  }
  const g = new THREE.BufferGeometry()
  g.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3))
  g.setAttribute('normal', new THREE.Float32BufferAttribute(nor, 3))
  g.setIndex(idx)
  return g
}

// PrintScene renders print geometry; G-code coordinates are Z-up and in mm.
export class PrintScene {
  private renderer: THREE.WebGLRenderer
  private scene = new THREE.Scene()
  private camera = new THREE.PerspectiveCamera(40, 1, 1, 5000)
  private controls: OrbitControls
  private root = new THREE.Group() // G-code space rotated to Three.js Y-up
  private bed: THREE.LineSegments | null = null
  private meshes: THREE.Object3D[] = []
  private materials: THREE.ShaderMaterial[] = []
  private geometry: THREE.InstancedBufferGeometry | null = null
  private uniforms = {
    uLayer: { value: 0 },
    uMode: { value: 0 },
    uTools: { value: Array.from({ length: MAX_TOOLS }, () => new THREE.Color(0x808080)) },
    uFeatures: { value: Array.from({ length: MAX_FEATURES }, () => new THREE.Color(0x808080)) },
    uOpacity: { value: 0.12 },
  }
  private resizeObserver: ResizeObserver
  private frame = 0
  lines = false

  constructor(private container: HTMLElement) {
    this.renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true })
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    this.renderer.setClearColor(0x000000, 0)
    container.appendChild(this.renderer.domElement)
    this.root.rotation.x = -Math.PI / 2
    this.scene.add(this.root)
    this.controls = new OrbitControls(this.camera, this.renderer.domElement)
    this.controls.addEventListener('change', () => this.render())
    this.resizeObserver = new ResizeObserver(() => this.resize())
    this.resizeObserver.observe(container)
    this.camera.position.set(0, 300, 400)
    this.resize()
  }

  setBed(width: number, depth: number) {
    if (this.bed) {
      this.root.remove(this.bed)
      this.setBedNone()
    }
    // Lines every 16 mm and along the far edges, which need not be on that grid.
    const step = 16
    const lines = (size: number) => {
      const at: number[] = []
      for (let p = 0; p < size; p += step) at.push(p)
      at.push(size)
      return at
    }
    const v: number[] = []
    for (const x of lines(width)) v.push(x, 0, 0, x, depth, 0)
    for (const y of lines(depth)) v.push(0, y, 0, width, y, 0)
    const g = new THREE.BufferGeometry()
    g.setAttribute('position', new THREE.Float32BufferAttribute(v, 3))
    this.bed = new THREE.LineSegments(g, new THREE.LineBasicMaterial({ color: 0x888888, transparent: true, opacity: 0.35 }))
    this.root.add(this.bed)
    if (!this.geometry) {
      this.fit([0, 0, 0, width, depth, 10])
    }
    this.render()
  }

  setGeometry(g: LoadedGeometry | null) {
    this.clearGeometry()
    if (!g || g.meta.points < 2) {
      this.render()
      return
    }
    const count = g.meta.points - 1
    const mobile = window.matchMedia('(pointer: coarse)').matches
    // "?lines" in the page URL forces the line view, for testing.
    const forced = new URLSearchParams(window.location.search).has('lines')
    this.lines = forced || count > (mobile ? SOLID_LIMIT_MOBILE : SOLID_LIMIT_DESKTOP)

    const geometry = new THREE.InstancedBufferGeometry()
    if (this.lines) {
      geometry.setAttribute('position', new THREE.Float32BufferAttribute([0, 0, 0, 1, 0, 0], 3))
    } else {
      const base = prism()
      geometry.index = base.index
      geometry.setAttribute('position', base.getAttribute('position'))
      geometry.setAttribute('normal', base.getAttribute('normal'))
    }
    const buffer = new THREE.InstancedInterleavedBuffer(g.points, POINT_FLOATS)
    geometry.setAttribute('iStart', new THREE.InterleavedBufferAttribute(buffer, 3, 0))
    geometry.setAttribute('iEnd', new THREE.InterleavedBufferAttribute(buffer, 3, POINT_FLOATS))
    geometry.setAttribute('iProps', new THREE.InterleavedBufferAttribute(buffer, 4, POINT_FLOATS + 3))
    geometry.instanceCount = count
    this.geometry = geometry

    g.meta.filaments.slice(0, MAX_TOOLS).forEach((c, i) => this.uniforms.uTools.value[i].setStyle(c))
    g.meta.features.slice(0, MAX_FEATURES).forEach((name, i) => this.uniforms.uFeatures.value[i].setStyle(featureColor(name)))

    // Opaque pass for printed layers, then the transparent ghost of the rest.
    for (const ghost of [false, true]) {
      const material = new THREE.ShaderMaterial({
        uniforms: { ...this.uniforms, uGhost: { value: ghost ? 1 : 0 } },
        vertexShader: this.lines ? lineVertex : solidVertex,
        fragmentShader: this.lines ? lineFragment : solidFragment,
        transparent: ghost,
        depthWrite: !ghost,
      })
      const mesh = this.lines ? new THREE.LineSegments(geometry, material) : new THREE.Mesh(geometry, material)
      mesh.frustumCulled = false // the instances are far outside the unit base geometry
      mesh.renderOrder = ghost ? 1 : 0
      this.materials.push(material)
      this.meshes.push(mesh)
      this.root.add(mesh)
    }
    this.fit(g.meta.bounds)
    this.render()
  }

  // setLayer sets the 0-based index of the layer shown as being printed;
  // -1 shows only the ghost and an index past the last layer shows everything printed.
  setLayer(index: number) {
    this.uniforms.uLayer.value = index
    this.render()
  }

  setColorMode(mode: ColorMode) {
    this.uniforms.uMode.value = mode === 'feature' ? 1 : 0
    this.render()
  }

  dispose() {
    cancelAnimationFrame(this.frame)
    this.resizeObserver.disconnect()
    this.controls.dispose()
    this.clearGeometry()
    this.setBedNone()
    this.renderer.dispose()
    this.renderer.domElement.remove()
  }

  private setBedNone() {
    if (this.bed) {
      this.bed.geometry.dispose()
      ;(this.bed.material as THREE.Material).dispose()
      this.bed = null
    }
  }

  private clearGeometry() {
    for (const m of this.meshes) this.root.remove(m)
    for (const m of this.materials) m.dispose()
    this.geometry?.dispose()
    this.meshes = []
    this.materials = []
    this.geometry = null
  }

  // fit points the camera at bounds given in G-code coordinates.
  private fit(b: number[]) {
    const center = new THREE.Vector3((b[0] + b[3]) / 2, (b[2] + b[5]) / 2, -(b[1] + b[4]) / 2)
    const radius = Math.max(10, new THREE.Vector3(b[3] - b[0], b[5] - b[2], b[4] - b[1]).length() / 2)
    const dist = (radius / Math.sin(THREE.MathUtils.degToRad(this.camera.fov / 2))) * 1.1
    this.camera.position.copy(center).add(new THREE.Vector3(0.45, 0.75, 1).normalize().multiplyScalar(dist))
    this.camera.near = dist / 100
    this.camera.far = dist * 10
    this.camera.updateProjectionMatrix()
    this.controls.target.copy(center)
    this.controls.update()
  }

  private resize() {
    const w = this.container.clientWidth
    const h = this.container.clientHeight
    if (w === 0 || h === 0) {
      return
    }
    this.renderer.setSize(w, h)
    this.camera.aspect = w / h
    this.camera.updateProjectionMatrix()
    this.render()
  }

  private render() {
    if (this.frame) {
      return
    }
    this.frame = requestAnimationFrame(() => {
      this.frame = 0
      this.renderer.render(this.scene, this.camera)
    })
  }
}
