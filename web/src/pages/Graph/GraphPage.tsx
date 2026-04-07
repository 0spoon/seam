import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { AnimatePresence, motion } from 'motion/react';
import cytoscape from 'cytoscape';
import fcose from 'cytoscape-fcose';
import { getGraph, getOrphanNotes } from '../../api/client';
import { useProjectStore } from '../../stores/projectStore';
import { useUIStore } from '../../stores/uiStore';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { getProjectColor } from '../../lib/tagColor';
import { GraphSkeleton } from '../../components/Skeleton/Skeleton';
import {
  Search,
  Filter,
  X,
  Crosshair,
  ArrowLeft,
  ExternalLink,
  Unlink,
} from 'lucide-react';
import type { GraphData, GraphNode } from '../../api/types';
import styles from './GraphPage.module.css';

// ---------------------------------------------------------------------------
// Cytoscape extension registration (guard against HMR double-registration)
// ---------------------------------------------------------------------------
try {
  cytoscape.use(fcose);
} catch {
  // Already registered
}

// ---------------------------------------------------------------------------
// Design tokens (Cytoscape cannot read CSS custom properties)
// ---------------------------------------------------------------------------
const COLORS = {
  accentPrimary: '#c4915c',
  accentMuted: 'rgba(196, 145, 92, 0.10)',
  borderDefault: '#2a3045',
  borderSubtle: '#1e2233',
  edgeDefault: '#3d4560',
  textPrimary: '#e8e2d9',
  textSecondary: '#a9a49b',
  bgDeep: '#08090d',
  bgSurface: '#161922',
} as const;

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------
const ZOOM_LABELS_NONE = 0.55;
const ZOOM_LABELS_HUBS = 1.0;
const HUB_DEGREE_MIN = 3;
const NODE_SIZE_MIN = 8;
const NODE_SIZE_MAX = 56;
const NODE_SIZE_SCALE = 10; // sqrt multiplier
const CLUSTER_COMPACT_FACTOR = 0.35;
const CLUSTER_PACK_GAP = 60;
const HULL_PADDING = 45;
const HULL_FILL_ALPHA = 0.05;
const HULL_STROKE_ALPHA = 0.12;
const HULL_LABEL_ALPHA = 0.5;
const DEFAULT_EDGE_OPACITY = 0.08;
const SEARCH_MAX_RESULTS = 10;

// ---------------------------------------------------------------------------
// Geometry utilities
// ---------------------------------------------------------------------------
interface Point {
  x: number;
  y: number;
}

function hexToRgba(hex: string, alpha: number): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

/** Cross product of vectors OA and OB (positive = counter-clockwise). */
function cross(O: Point, A: Point, B: Point): number {
  return (A.x - O.x) * (B.y - O.y) - (A.y - O.y) * (B.x - O.x);
}

/** Monotone chain convex hull -- returns vertices in counter-clockwise order. */
function convexHull(points: Point[]): Point[] {
  if (points.length <= 1) return [...points];
  const sorted = [...points].sort((a, b) => a.x - b.x || a.y - b.y);
  if (sorted.length <= 2) return sorted;

  const lower: Point[] = [];
  for (const p of sorted) {
    while (lower.length >= 2 && cross(lower[lower.length - 2], lower[lower.length - 1], p) <= 0) {
      lower.pop();
    }
    lower.push(p);
  }

  const upper: Point[] = [];
  for (let i = sorted.length - 1; i >= 0; i--) {
    const p = sorted[i];
    while (upper.length >= 2 && cross(upper[upper.length - 2], upper[upper.length - 1], p) <= 0) {
      upper.pop();
    }
    upper.push(p);
  }

  // Remove last point of each half (it is repeated as the first of the other).
  lower.pop();
  upper.pop();
  return lower.concat(upper);
}

// ---------------------------------------------------------------------------
// Circle packing -- spread clusters across the viewport
// ---------------------------------------------------------------------------

interface PackCircle {
  id: string;
  r: number;
  x: number;
  y: number;
}

/**
 * Pack circles into a rectangle so they fill the space with no overlaps.
 *
 * Algorithm:
 * 1. Sort circles largest-first.
 * 2. Place the first circle at the center.
 * 3. For each subsequent circle, find the position along the frontier of
 *    already-placed circles that is closest to the rectangle center while
 *    not overlapping any placed circle. We sample candidate positions
 *    tangent to each placed circle at angles spread around 360 degrees.
 * 4. After initial placement, run a spreading pass that pushes all circles
 *    outward from the global centroid so they fill the available rectangle,
 *    stopping when any circle would exit the bounds.
 */
function packCirclesInRect(
  circles: PackCircle[],
  width: number,
  height: number,
  gap: number,
): PackCircle[] {
  if (circles.length === 0) return [];

  // Work with copies sorted largest first.
  const sorted = circles
    .map((c) => ({ ...c }))
    .sort((a, b) => b.r - a.r);

  const cx = 0;
  const cy = 0;
  const placed: PackCircle[] = [];

  // Check if circle c overlaps any placed circle (including gap).
  const overlapsAny = (c: PackCircle): boolean =>
    placed.some((p) => {
      const dx = c.x - p.x;
      const dy = c.y - p.y;
      return Math.sqrt(dx * dx + dy * dy) < c.r + p.r + gap;
    });

  // Place first circle at center.
  sorted[0].x = cx;
  sorted[0].y = cy;
  placed.push(sorted[0]);

  // Place remaining circles.
  const ANGLE_SAMPLES = 36;
  for (let i = 1; i < sorted.length; i++) {
    const c = sorted[i];
    let bestX = cx;
    let bestY = cy;
    let bestDist = Infinity;

    // Try positions tangent to each already-placed circle.
    for (const p of placed) {
      const tangentDist = p.r + c.r + gap;
      for (let a = 0; a < ANGLE_SAMPLES; a++) {
        const angle = (a / ANGLE_SAMPLES) * Math.PI * 2;
        const candidateX = p.x + Math.cos(angle) * tangentDist;
        const candidateY = p.y + Math.sin(angle) * tangentDist;

        c.x = candidateX;
        c.y = candidateY;

        if (!overlapsAny(c)) {
          // Prefer positions that spread across the rectangle.
          // Score = distance from center (we want moderate spread, not all
          // at center), penalized if too far from center.
          const distFromCenter = Math.sqrt(
            candidateX * candidateX + candidateY * candidateY,
          );
          // Favor filling space: slight preference for positions further out,
          // but not beyond half the viewport dimension.
          const idealDist = Math.min(width, height) * 0.3;
          const score = Math.abs(distFromCenter - idealDist);
          if (score < bestDist) {
            bestDist = score;
            bestX = candidateX;
            bestY = candidateY;
          }
        }
      }
    }

    c.x = bestX;
    c.y = bestY;
    placed.push(c);
  }

  // Spreading pass: scale positions outward from centroid to fill the rect.
  const gcx = placed.reduce((s, c) => s + c.x, 0) / placed.length;
  const gcy = placed.reduce((s, c) => s + c.y, 0) / placed.length;

  // Find the maximum scale factor such that all circles stay within bounds.
  const halfW = width / 2;
  const halfH = height / 2;
  let maxScale = 10;
  for (const c of placed) {
    const dx = c.x - gcx;
    const dy = c.y - gcy;
    if (Math.abs(dx) > 0.01) {
      const sx = (halfW - c.r - gap) / Math.abs(dx);
      maxScale = Math.min(maxScale, sx);
    }
    if (Math.abs(dy) > 0.01) {
      const sy = (halfH - c.r - gap) / Math.abs(dy);
      maxScale = Math.min(maxScale, sy);
    }
  }

  // Apply spread (but cap at reasonable factor to avoid flinging single nodes).
  const spreadFactor = Math.max(1, Math.min(maxScale, 4));
  for (const c of placed) {
    c.x = gcx + (c.x - gcx) * spreadFactor;
    c.y = gcy + (c.y - gcy) * spreadFactor;
  }

  // Build result map keyed by id.
  return placed;
}

// ---------------------------------------------------------------------------
// Hull drawing
// ---------------------------------------------------------------------------

/** Draw cluster hulls for each project group onto the given canvas. */
function drawClusterHulls(
  canvas: HTMLCanvasElement,
  cy: cytoscape.Core,
  colorMap: Map<string, string>,
  nameMap: Map<string, string>,
) {
  const container = canvas.parentElement;
  if (!container) return;

  const rect = container.getBoundingClientRect();
  const dpr = window.devicePixelRatio || 1;
  canvas.width = rect.width * dpr;
  canvas.height = rect.height * dpr;
  canvas.style.width = `${rect.width}px`;
  canvas.style.height = `${rect.height}px`;

  const ctx = canvas.getContext('2d');
  if (!ctx) return;
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, rect.width, rect.height);

  // Group visible nodes by project.
  const groups = new Map<string, cytoscape.NodeSingular[]>();
  cy.nodes(':visible').forEach((node) => {
    const pid = node.data('projectId') as string;
    if (!pid) return;
    if (!groups.has(pid)) groups.set(pid, []);
    groups.get(pid)!.push(node);
  });

  groups.forEach((nodes, pid) => {
    if (nodes.length < 2) return;

    const color = colorMap.get(pid) || COLORS.accentPrimary;
    const name = nameMap.get(pid) || '';
    const points: Point[] = nodes.map((n) => n.renderedPosition());

    // Centroid for inflation direction and label placement.
    const cx = points.reduce((s, p) => s + p.x, 0) / points.length;
    const cy2 = points.reduce((s, p) => s + p.y, 0) / points.length;

    const hull = convexHull(points);

    // Inflate hull outward from centroid.
    const inflated = hull.map((p) => {
      const dx = p.x - cx;
      const dy = p.y - cy2;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      return {
        x: p.x + (dx / dist) * HULL_PADDING,
        y: p.y + (dy / dist) * HULL_PADDING,
      };
    });

    // Draw smooth path using midpoint quadratic curves.
    ctx.beginPath();
    if (inflated.length >= 3) {
      const last = inflated[inflated.length - 1];
      const first = inflated[0];
      ctx.moveTo((last.x + first.x) / 2, (last.y + first.y) / 2);
      for (let i = 0; i < inflated.length; i++) {
        const curr = inflated[i];
        const next = inflated[(i + 1) % inflated.length];
        ctx.quadraticCurveTo(curr.x, curr.y, (curr.x + next.x) / 2, (curr.y + next.y) / 2);
      }
    } else {
      // Two points: draw ellipse between them.
      const p1 = inflated[0];
      const p2 = inflated[1];
      const mx = (p1.x + p2.x) / 2;
      const my = (p1.y + p2.y) / 2;
      const dx = p2.x - p1.x;
      const dy = p2.y - p1.y;
      const dist = Math.sqrt(dx * dx + dy * dy);
      const angle = Math.atan2(dy, dx);
      ctx.save();
      ctx.translate(mx, my);
      ctx.rotate(angle);
      ctx.beginPath();
      ctx.ellipse(0, 0, dist / 2 + HULL_PADDING, HULL_PADDING, 0, 0, Math.PI * 2);
      ctx.restore();
    }
    ctx.closePath();

    ctx.fillStyle = hexToRgba(color, HULL_FILL_ALPHA);
    ctx.fill();
    ctx.strokeStyle = hexToRgba(color, HULL_STROKE_ALPHA);
    ctx.lineWidth = 1;
    ctx.stroke();

    // Cluster label above the hull.
    if (name) {
      const topY = Math.min(...points.map((p) => p.y));
      ctx.font = '500 13px Outfit, system-ui, sans-serif';
      ctx.fillStyle = hexToRgba(color, HULL_LABEL_ALPHA);
      ctx.textAlign = 'center';
      ctx.textBaseline = 'bottom';
      ctx.fillText(name, cx, topY - HULL_PADDING - 6);
    }
  });
}

// ---------------------------------------------------------------------------
// Neighborhood BFS (for focus mode)
// ---------------------------------------------------------------------------

interface DisplayData {
  nodes: GraphNode[];
  edges: { source: string; target: string }[];
  /** Maps node ID -> hop distance from focus node. Null when not in focus mode. */
  hopMap: Map<string, number> | null;
}

function buildDisplayData(
  graphData: GraphData,
  focusNodeId: string | null,
): DisplayData {
  if (!focusNodeId) {
    return { nodes: graphData.nodes, edges: graphData.edges, hopMap: null };
  }

  // BFS from focus node, up to 2 hops.
  const hopMap = new Map<string, number>();
  hopMap.set(focusNodeId, 0);

  const adj = new Map<string, string[]>();
  for (const edge of graphData.edges) {
    if (!adj.has(edge.source)) adj.set(edge.source, []);
    if (!adj.has(edge.target)) adj.set(edge.target, []);
    adj.get(edge.source)!.push(edge.target);
    adj.get(edge.target)!.push(edge.source);
  }

  const queue = [focusNodeId];
  while (queue.length > 0) {
    const current = queue.shift()!;
    const hop = hopMap.get(current)!;
    if (hop >= 2) continue;
    for (const neighbor of adj.get(current) || []) {
      if (!hopMap.has(neighbor)) {
        hopMap.set(neighbor, hop + 1);
        queue.push(neighbor);
      }
    }
  }

  const visible = new Set(hopMap.keys());
  return {
    nodes: graphData.nodes.filter((n) => visible.has(n.id)),
    edges: graphData.edges.filter(
      (e) => visible.has(e.source) && visible.has(e.target),
    ),
    hopMap,
  };
}

// ---------------------------------------------------------------------------
// Tooltip data
// ---------------------------------------------------------------------------
interface TooltipData {
  title: string;
  project?: string;
  projectColor?: string;
  tags: string[];
  linkCount: number;
  date: string;
  x: number;
  y: number;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------
export function GraphPage() {
  const navigate = useNavigate();

  // Refs
  const containerRef = useRef<HTMLDivElement>(null);
  const minimapRef = useRef<HTMLDivElement>(null);
  const hullCanvasRef = useRef<HTMLCanvasElement>(null);
  const cyRef = useRef<cytoscape.Core | null>(null);
  const minimapCyRef = useRef<cytoscape.Core | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const zoomTierRef = useRef(-1);

  // Store data
  const projects = useProjectStore((s) => s.projects);
  const tags = useUIStore((s) => s.tags);
  const addToast = useToastStore((s) => s.addToast);

  // Data state
  const [graphData, setGraphData] = useState<GraphData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [orphanCount, setOrphanCount] = useState(0);

  // Filter state
  const [filterOpen, setFilterOpen] = useState(false);
  const [selectedProjects, setSelectedProjects] = useState<Set<string>>(new Set());
  const [activeTags, setActiveTags] = useState<Set<string>>(new Set());
  const [sinceDate, setSinceDate] = useState('');
  const [untilDate, setUntilDate] = useState('');

  // Interaction state
  const [tooltip, setTooltip] = useState<TooltipData | null>(null);
  const [selectedNode, setSelectedNode] = useState<{
    id: string;
    title: string;
  } | null>(null);
  const [focusNodeId, setFocusNodeId] = useState<string | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedSearchIdx, setSelectedSearchIdx] = useState(0);
  const [focusedNodeIndex, setFocusedNodeIndex] = useState(-1);

  // ----- Derived data -----

  const projectColorMap = useCallback(() => {
    const map = new Map<string, string>();
    projects.forEach((p, i) => {
      map.set(p.id, getProjectColor(i));
    });
    return map;
  }, [projects]);

  const projectNameMap = useMemo(() => {
    const map = new Map<string, string>();
    if (graphData) {
      for (const n of graphData.nodes) {
        if (n.project_id && n.project) {
          map.set(n.project_id, n.project);
        }
      }
    }
    return map;
  }, [graphData]);

  const displayData = useMemo<DisplayData | null>(() => {
    if (!graphData) return null;
    return buildDisplayData(graphData, focusNodeId);
  }, [graphData, focusNodeId]);

  const searchResults = useMemo(() => {
    if (!searchQuery.trim() || !graphData) return [];
    const q = searchQuery.toLowerCase();
    return graphData.nodes
      .filter((n) => n.title.toLowerCase().includes(q))
      .slice(0, SEARCH_MAX_RESULTS);
  }, [searchQuery, graphData]);

  const activeFilterCount =
    selectedProjects.size + activeTags.size + (sinceDate ? 1 : 0) + (untilDate ? 1 : 0);

  // Visible node/edge counts for stats bar.
  const visibleCounts = useMemo(() => {
    if (!displayData) return { nodes: 0, edges: 0, projects: 0 };
    const pids = new Set<string>();
    for (const n of displayData.nodes) {
      if (n.project_id) pids.add(n.project_id);
    }
    return {
      nodes: displayData.nodes.length,
      edges: displayData.edges.length,
      projects: pids.size,
    };
  }, [displayData]);

  // ----- Data fetching -----

  const fetchGraph = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getGraph({
        since: sinceDate ? new Date(sinceDate).toISOString() : undefined,
        until: untilDate ? new Date(untilDate).toISOString() : undefined,
      });
      setGraphData(data);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to load graph data';
      setError(message);
      addToast(message, 'error');
    } finally {
      setLoading(false);
    }
  }, [sinceDate, untilDate, addToast]);

  useEffect(() => {
    fetchGraph();
  }, [fetchGraph]);

  // Fetch orphan count once on mount.
  useEffect(() => {
    let cancelled = false;
    getOrphanNotes()
      .then((orphans) => {
        if (!cancelled) setOrphanCount(orphans.length);
      })
      .catch(() => {
        // Non-critical, ignore.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // ----- Cytoscape initialization -----

  useEffect(() => {
    if (!containerRef.current || !displayData) return;

    const colors = projectColorMap();
    const inFocusMode = focusNodeId !== null;

    // Build elements.
    const nodes: cytoscape.ElementDefinition[] = displayData.nodes.map(
      (node) => {
        const color = node.project_id
          ? (colors.get(node.project_id) ?? COLORS.accentPrimary)
          : COLORS.accentPrimary;
        const linkCount = node.link_count ?? 0;
        const size = Math.min(
          NODE_SIZE_MIN + Math.sqrt(linkCount) * NODE_SIZE_SCALE,
          NODE_SIZE_MAX,
        );
        return {
          data: {
            id: node.id,
            label: node.title,
            projectId: node.project_id || '',
            projectName: node.project || '',
            color,
            size,
            tags: node.tags || [],
            createdAt: node.created_at,
          },
        };
      },
    );

    const edges: cytoscape.ElementDefinition[] = displayData.edges.map(
      (edge, i) => ({
        data: {
          id: `e${i}`,
          source: edge.source,
          target: edge.target,
        },
      }),
    );

    // Destroy previous instances.
    if (cyRef.current) cyRef.current.destroy();
    if (minimapCyRef.current) minimapCyRef.current.destroy();

    const prefersReducedMotion =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    // Choose layout based on mode.
    const layout: cytoscape.LayoutOptions = inFocusMode
      ? ({
          name: 'concentric',
          concentric: (node: cytoscape.NodeSingular) => {
            const hop = displayData.hopMap?.get(node.id());
            if (hop === 0) return 100;
            if (hop === 1) return 50;
            return 1;
          },
          levelWidth: () => 1,
          animate: !prefersReducedMotion,
          animationDuration: 400,
          minNodeSpacing: 50,
        } as cytoscape.LayoutOptions)
      : ({
          name: 'fcose',
          animate: !prefersReducedMotion,
          animationDuration: 600,
          quality: 'proof',
          nodeSeparation: 200,
          idealEdgeLength: 220,
          nodeRepulsion: () => 55000,
          edgeElasticity: () => 0.45,
          gravity: 0.04,
          gravityRange: 1.0,
          numIter: 5000,
          tile: true,
          tilingPaddingVertical: 40,
          tilingPaddingHorizontal: 40,
        } as cytoscape.LayoutOptions);

    const cy = cytoscape({
      container: containerRef.current,
      elements: [...nodes, ...edges],
      style: [
        // -- Base node style -----------------------------------------------
        {
          selector: 'node',
          style: {
            'background-color': ((ele: cytoscape.NodeSingular) =>
              ele.data('color')) as unknown as string,
            'background-opacity': 0.85,
            'border-width': 0,
            shape: 'ellipse',
            width: 'data(size)',
            height: 'data(size)',
            label: 'data(label)',
            'font-family': 'Outfit, system-ui, sans-serif',
            'font-size': '10px',
            'font-weight': 400,
            color: COLORS.textSecondary,
            'text-valign': 'bottom',
            'text-halign': 'center',
            'text-margin-y': 8,
            'text-max-width': '120px',
            'text-wrap': 'ellipsis',
            'text-outline-color': COLORS.bgDeep,
            'text-outline-width': 2,
            'text-outline-opacity': 0.9,
            'text-opacity': 0,
            'overlay-padding': 6,
          } as cytoscape.Css.Node,
        },
        // Class: labels hidden (semantic zoom -- overview)
        {
          selector: 'node.label-hidden',
          style: { 'text-opacity': 0 } as cytoscape.Css.Node,
        },
        // Class: labels for hubs only (semantic zoom -- mid)
        {
          selector: 'node.label-hub',
          style: {
            'text-opacity': 1,
            'font-size': '11px',
          } as cytoscape.Css.Node,
        },
        // Class: all labels visible (semantic zoom -- close)
        {
          selector: 'node.label-all',
          style: { 'text-opacity': 1 } as cytoscape.Css.Node,
        },
        // Class: dimmed (non-neighbor during hover/select)
        {
          selector: 'node.dimmed',
          style: {
            'background-opacity': 0.12,
            'text-opacity': 0,
          } as cytoscape.Css.Node,
        },
        // Class: hovered node
        {
          selector: 'node.hovered',
          style: {
            'background-opacity': 1,
            'border-width': 2,
            'border-color': ((ele: cytoscape.NodeSingular) =>
              ele.data('color')) as unknown as string,
            'border-opacity': 0.8,
            color: COLORS.textPrimary,
            'font-weight': 500,
            'text-opacity': 1,
          } as cytoscape.Css.Node,
        },
        // Selected node (highest class priority in Cytoscape)
        {
          selector: 'node:selected',
          style: {
            'background-opacity': 1,
            'border-width': 2,
            'border-color': COLORS.accentPrimary,
            'border-opacity': 1,
            color: COLORS.textPrimary,
            'font-weight': 500,
            'text-opacity': 1,
          } as cytoscape.Css.Node,
        },
        // -- Base edge style -----------------------------------------------
        {
          selector: 'edge',
          style: {
            'line-color': COLORS.edgeDefault,
            width: 1,
            opacity: DEFAULT_EDGE_OPACITY,
            'curve-style': 'straight',
            'target-arrow-shape': 'none',
          } as cytoscape.Css.Edge,
        },
        // Class: highlighted edge (connected to hovered/selected node)
        {
          selector: 'edge.highlighted',
          style: {
            'line-color': COLORS.accentPrimary,
            'target-arrow-color': COLORS.accentPrimary,
            width: 1.5,
            opacity: 0.7,
          } as cytoscape.Css.Edge,
        },
        // Class: dimmed edge
        {
          selector: 'edge.dimmed',
          style: {
            opacity: 0.02,
          } as cytoscape.Css.Edge,
        },
        // Selected edge
        {
          selector: 'edge:selected',
          style: {
            'line-color': COLORS.accentPrimary,
            'target-arrow-color': COLORS.accentPrimary,
            width: 2,
            opacity: 1,
          } as cytoscape.Css.Edge,
        },
      ],
      layout,
      minZoom: 0.15,
      maxZoom: 5,
      wheelSensitivity: 0.3,
    });

    // ---- Semantic zoom (label level-of-detail) ----

    zoomTierRef.current = -1;

    const updateLabelVisibility = () => {
      const zoom = cy.zoom();
      let tier: number;
      if (inFocusMode) {
        tier = 2; // Always show labels in focus mode.
      } else if (zoom < ZOOM_LABELS_NONE) {
        tier = 0;
      } else if (zoom < ZOOM_LABELS_HUBS) {
        tier = 1;
      } else {
        tier = 2;
      }

      if (tier === zoomTierRef.current) return;
      zoomTierRef.current = tier;

      cy.startBatch();
      cy.nodes().removeClass('label-hidden label-hub label-all');
      if (tier === 0) {
        cy.nodes().addClass('label-hidden');
      } else if (tier === 1) {
        cy.nodes().forEach((node) => {
          if (node.degree(false) >= HUB_DEGREE_MIN) {
            node.addClass('label-hub');
          } else {
            node.addClass('label-hidden');
          }
        });
      } else {
        cy.nodes().addClass('label-all');
      }
      cy.endBatch();
    };

    cy.on('zoom', updateLabelVisibility);

    // ---- Interaction: hover ----

    const applyNeighborhoodHighlight = (node: cytoscape.NodeSingular) => {
      const neighbors = node.neighborhood('node');
      const connectedEdges = node.connectedEdges();

      cy.startBatch();
      cy.nodes().not(node).not(neighbors).addClass('dimmed');
      connectedEdges.addClass('highlighted');
      cy.edges().not(connectedEdges).addClass('dimmed');
      cy.endBatch();
    };

    const clearHighlight = () => {
      cy.startBatch();
      cy.elements().removeClass('dimmed highlighted hovered');
      cy.endBatch();
    };

    const reapplySelectionHighlight = () => {
      const sel = cy.nodes(':selected');
      if (sel.length > 0) {
        applyNeighborhoodHighlight(sel[0]);
      }
    };

    cy.on('mouseover', 'node', (evt) => {
      const node = evt.target;
      node.addClass('hovered');

      // Highlight neighborhood.
      clearHighlight();
      node.addClass('hovered');
      applyNeighborhoodHighlight(node);

      // Tooltip.
      const pos = node.renderedPosition();
      const renderedH = node.renderedHeight();
      const linkCount = node.connectedEdges().length;
      const nodeTags: string[] = node.data('tags') || [];
      const projectName: string = node.data('projectName') || '';
      const projectColor: string = node.data('color') || '';
      const createdAt: string = node.data('createdAt') || '';

      let dateStr = '';
      if (createdAt) {
        try {
          dateStr = new Date(createdAt).toLocaleDateString('en-US', {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
          });
        } catch {
          // Ignore date parse errors.
        }
      }

      setTooltip({
        title: node.data('label'),
        project: projectName || undefined,
        projectColor: projectName ? projectColor : undefined,
        tags: nodeTags,
        linkCount,
        date: dateStr,
        x: pos.x,
        y: pos.y - renderedH / 2 - 14,
      });
    });

    cy.on('mouseout', 'node', () => {
      clearHighlight();
      reapplySelectionHighlight();
      setTooltip(null);
    });

    // ---- Interaction: tap (select / deselect) ----

    cy.on('tap', 'node', (evt) => {
      const node = evt.target;
      cy.elements().unselect();
      clearHighlight();
      node.select();
      applyNeighborhoodHighlight(node);
      setSelectedNode({ id: node.id(), title: node.data('label') });
    });

    cy.on('tap', (evt) => {
      if (evt.target === cy) {
        cy.elements().unselect();
        clearHighlight();
        setSelectedNode(null);
      }
    });

    // Double-click to navigate.
    cy.on('dbltap', 'node', (evt) => {
      navigate(`/notes/${evt.target.id()}`);
    });

    // ---- Post-layout: cluster nudging + hull drawing ----

    const drawHulls = () => {
      if (!hullCanvasRef.current || !cyRef.current || inFocusMode) {
        // Clear canvas in focus mode.
        if (hullCanvasRef.current) {
          const ctx = hullCanvasRef.current.getContext('2d');
          if (ctx) {
            ctx.clearRect(0, 0, hullCanvasRef.current.width, hullCanvasRef.current.height);
          }
        }
        return;
      }
      drawClusterHulls(hullCanvasRef.current, cyRef.current, colors, projectNameMap);
    };

    cy.one('layoutstop', () => {
      if (!inFocusMode) {
        // ---------------------------------------------------------------
        // Post-layout cluster placement: pack clusters across the viewport
        // with maximum spread and minimal overlap.
        // ---------------------------------------------------------------

        // Step 1: Group nodes by project, compute centroid and radius.
        const clusterNodes = new Map<string, cytoscape.NodeSingular[]>();
        const orphanNodes: cytoscape.NodeSingular[] = [];

        cy.nodes().forEach((node) => {
          const pid = node.data('projectId') as string;
          if (!pid) {
            orphanNodes.push(node);
            return;
          }
          if (!clusterNodes.has(pid)) clusterNodes.set(pid, []);
          clusterNodes.get(pid)!.push(node);
        });

        const clusterInfo = new Map<
          string,
          { cx: number; cy: number; r: number; nodes: cytoscape.NodeSingular[] }
        >();

        clusterNodes.forEach((nodes, pid) => {
          const cx2 = nodes.reduce((s, n) => s + n.position().x, 0) / nodes.length;
          const cy2 = nodes.reduce((s, n) => s + n.position().y, 0) / nodes.length;

          // Radius = max distance from centroid to any node + node half-size.
          let maxDist = 0;
          nodes.forEach((n) => {
            const dx = n.position().x - cx2;
            const dy = n.position().y - cy2;
            const dist = Math.sqrt(dx * dx + dy * dy) + (n.data('size') as number) / 2;
            if (dist > maxDist) maxDist = dist;
          });

          clusterInfo.set(pid, { cx: cx2, cy: cy2, r: maxDist, nodes });
        });

        // Step 2: Compact each cluster -- pull nodes toward their centroid
        // to tighten the group before we reposition.
        clusterInfo.forEach((info) => {
          info.nodes.forEach((node) => {
            const pos = node.position();
            node.position({
              x: pos.x + (info.cx - pos.x) * CLUSTER_COMPACT_FACTOR,
              y: pos.y + (info.cy - pos.y) * CLUSTER_COMPACT_FACTOR,
            });
          });

          // Recompute centroid and radius after compaction.
          const cx2 =
            info.nodes.reduce((s, n) => s + n.position().x, 0) / info.nodes.length;
          const cy2 =
            info.nodes.reduce((s, n) => s + n.position().y, 0) / info.nodes.length;
          let maxDist = 0;
          info.nodes.forEach((n) => {
            const dx = n.position().x - cx2;
            const dy = n.position().y - cy2;
            const dist = Math.sqrt(dx * dx + dy * dy) + (n.data('size') as number) / 2;
            if (dist > maxDist) maxDist = dist;
          });
          info.cx = cx2;
          info.cy = cy2;
          info.r = maxDist;
        });

        // Step 3: Use circle packing to assign target positions.
        // Use the viewport size (model coords estimated from container).
        const container = containerRef.current;
        const viewW = container ? container.clientWidth * 3.5 : 4000;
        const viewH = container ? container.clientHeight * 3.5 : 3000;

        const packInput: PackCircle[] = [];
        clusterInfo.forEach((info, pid) => {
          packInput.push({ id: pid, r: info.r, x: 0, y: 0 });
        });

        const packed = packCirclesInRect(packInput, viewW, viewH, CLUSTER_PACK_GAP);
        const targetPositions = new Map<string, { x: number; y: number }>();
        packed.forEach((c) => targetPositions.set(c.id, { x: c.x, y: c.y }));

        // Step 4: Translate each cluster's nodes from current centroid to
        // the packed target position.
        clusterInfo.forEach((info, pid) => {
          const target = targetPositions.get(pid);
          if (!target) return;
          const dx = target.x - info.cx;
          const dy = target.y - info.cy;
          info.nodes.forEach((node) => {
            const pos = node.position();
            node.position({ x: pos.x + dx, y: pos.y + dy });
          });
        });

        // Step 5: Spread orphan nodes (no project) along the bottom edge.
        if (orphanNodes.length > 0) {
          const allPacked = packed.length > 0;
          const baseY = allPacked
            ? Math.max(...packed.map((c) => c.y + c.r)) + 150
            : 0;
          const spacing = 80;
          const startX = -((orphanNodes.length - 1) * spacing) / 2;
          orphanNodes.forEach((node, i) => {
            node.position({ x: startX + i * spacing, y: baseY });
          });
        }
      }

      // Fit with generous padding and clamp zoom.
      cy.fit(undefined, 60);
      const maxZoom = 2.2;
      if (cy.zoom() > maxZoom) {
        cy.zoom(maxZoom);
        cy.center();
      }

      updateLabelVisibility();
      drawHulls();
    });

    cy.on('pan zoom resize', drawHulls);

    cyRef.current = cy;

    // ---- Minimap ----

    if (minimapRef.current) {
      const minimapCy = cytoscape({
        container: minimapRef.current,
        elements: cy.elements().jsons() as cytoscape.ElementDefinition[],
        style: [
          {
            selector: 'node',
            style: {
              'background-color': COLORS.accentPrimary,
              'background-opacity': 0.7,
              width: 4,
              height: 4,
              shape: 'ellipse',
              label: '',
            } as cytoscape.Css.Node,
          },
          {
            selector: 'edge',
            style: {
              'line-color': COLORS.borderSubtle,
              width: 0.5,
              opacity: 0.25,
              'target-arrow-shape': 'none',
            } as cytoscape.Css.Edge,
          },
        ],
        layout: { name: 'preset' },
        userZoomingEnabled: false,
        userPanningEnabled: false,
        boxSelectionEnabled: false,
        autoungrabify: true,
        autounselectify: true,
      });

      minimapCy.fit(undefined, 4);
      minimapCyRef.current = minimapCy;

      // Minimap viewport rectangle.
      let minimapTimer: ReturnType<typeof setTimeout> | undefined;
      const updateMinimapImmediate = () => {
        if (!minimapCyRef.current || !cyRef.current) return;
        const mc = minimapCyRef.current;

        cyRef.current.nodes().forEach((node) => {
          const pos = node.position();
          const mn = mc.getElementById(node.id());
          if (mn.length > 0) mn.position(pos);
        });
        mc.fit(undefined, 4);

        const ext = cyRef.current.extent();
        const viewportId = '__viewport_rect__';
        const existing = mc.getElementById(viewportId);
        if (existing.length > 0) {
          existing.position({
            x: (ext.x1 + ext.x2) / 2,
            y: (ext.y1 + ext.y2) / 2,
          });
          existing.style({
            width: ext.x2 - ext.x1,
            height: ext.y2 - ext.y1,
          });
        } else {
          mc.add({
            group: 'nodes',
            data: { id: viewportId },
            position: {
              x: (ext.x1 + ext.x2) / 2,
              y: (ext.y1 + ext.y2) / 2,
            },
            style: {
              width: ext.x2 - ext.x1,
              height: ext.y2 - ext.y1,
              shape: 'rectangle',
              'background-color': 'transparent',
              'border-width': 1,
              'border-color': COLORS.accentPrimary,
              'border-opacity': 0.6,
              'background-opacity': 0.05,
              label: '',
              events: 'no',
            } as unknown as cytoscape.Css.Node,
          });
        }
      };

      const updateMinimap = () => {
        if (minimapTimer !== undefined) return;
        minimapTimer = setTimeout(() => {
          minimapTimer = undefined;
          updateMinimapImmediate();
        }, 50);
      };

      cy.on('pan zoom', updateMinimap);
      cy.on('position', 'node', updateMinimap);
      cy.on('add remove', updateMinimap);
      cy.on('layoutstop', updateMinimap);
      updateMinimapImmediate();

      // Interactive minimap: click to navigate.
      const handleMinimapClick = (evt: MouseEvent) => {
        if (!cyRef.current || !minimapCyRef.current || !minimapRef.current) return;
        const mc = minimapCyRef.current;
        const rect = minimapRef.current.getBoundingClientRect();
        const renderedX = evt.clientX - rect.left;
        const renderedY = evt.clientY - rect.top;

        // Convert minimap rendered position to model coordinates.
        const modelX = (renderedX - mc.pan().x) / mc.zoom();
        const modelY = (renderedY - mc.pan().y) / mc.zoom();

        // Pan main graph to center on these model coordinates.
        const mainZoom = cyRef.current.zoom();
        cyRef.current.animate({
          pan: {
            x: cyRef.current.width() / 2 - modelX * mainZoom,
            y: cyRef.current.height() / 2 - modelY * mainZoom,
          },
          duration: 200,
        });
      };

      let minimapDragging = false;
      const onMinimapDown = (e: MouseEvent) => {
        minimapDragging = true;
        handleMinimapClick(e);
      };
      const onMinimapMove = (e: MouseEvent) => {
        if (minimapDragging) handleMinimapClick(e);
      };
      const onMinimapUp = () => {
        minimapDragging = false;
      };

      const mmEl = minimapRef.current;
      mmEl.addEventListener('mousedown', onMinimapDown);
      mmEl.addEventListener('mousemove', onMinimapMove);
      document.addEventListener('mouseup', onMinimapUp);

      // Cleanup minimap listeners when the effect tears down.
      const cleanupMinimap = () => {
        mmEl.removeEventListener('mousedown', onMinimapDown);
        mmEl.removeEventListener('mousemove', onMinimapMove);
        document.removeEventListener('mouseup', onMinimapUp);
      };

      // Store cleanup so the main cleanup can call it.
      (cy as unknown as Record<string, unknown>).__cleanupMinimap = cleanupMinimap;
    }

    return () => {
      const cleanupMm = (cy as unknown as Record<string, unknown>).__cleanupMinimap as
        | (() => void)
        | undefined;
      if (cleanupMm) cleanupMm();
      cy.destroy();
      cyRef.current = null;
      if (minimapCyRef.current) {
        minimapCyRef.current.destroy();
        minimapCyRef.current = null;
      }
    };
  }, [displayData, projectColorMap, projectNameMap, navigate, focusNodeId]);

  // ----- Client-side filters (project + tag) -----

  useEffect(() => {
    if (!cyRef.current) return;
    const cy = cyRef.current;
    const hasProjectFilter = selectedProjects.size > 0;
    const hasTagFilter = activeTags.size > 0;

    cy.startBatch();
    cy.nodes().forEach((node) => {
      let visible = true;

      if (hasProjectFilter) {
        const pid = node.data('projectId');
        if (!pid || !selectedProjects.has(pid)) visible = false;
      }

      if (hasTagFilter && visible) {
        const nodeTags: string[] = node.data('tags') || [];
        if (!nodeTags.some((t) => activeTags.has(t))) visible = false;
      }

      if (visible) {
        node.style('display', 'element');
      } else {
        node.style('display', 'none');
      }
    });

    cy.edges().forEach((edge) => {
      if (edge.source().hidden() || edge.target().hidden()) {
        edge.style('display', 'none');
      } else {
        edge.style('display', 'element');
      }
    });
    cy.endBatch();

    // Redraw hulls after filter change.
    if (hullCanvasRef.current && cyRef.current && !focusNodeId) {
      drawClusterHulls(
        hullCanvasRef.current,
        cyRef.current,
        projectColorMap(),
        projectNameMap,
      );
    }
  }, [selectedProjects, activeTags, projectColorMap, projectNameMap, focusNodeId]);

  // ----- Global keyboard shortcut for search -----

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (
        target instanceof HTMLInputElement ||
        target instanceof HTMLTextAreaElement ||
        target.isContentEditable
      ) {
        return;
      }
      if (e.key === '/' && !searchOpen) {
        e.preventDefault();
        setSearchOpen(true);
        setTimeout(() => searchInputRef.current?.focus(), 0);
      }
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [searchOpen]);

  // Reset search index when results change.
  useEffect(() => {
    setSelectedSearchIdx(0);
  }, [searchResults]);

  // ----- Handlers -----

  const handleProjectToggle = (projectId: string) => {
    setSelectedProjects((prev) => {
      const next = new Set(prev);
      if (next.has(projectId)) next.delete(projectId);
      else next.add(projectId);
      return next;
    });
  };

  const handleTagToggle = (tagName: string) => {
    setActiveTags((prev) => {
      const next = new Set(prev);
      if (next.has(tagName)) next.delete(tagName);
      else next.add(tagName);
      return next;
    });
  };

  const handleReset = () => {
    setActiveTags(new Set());
    setSelectedProjects(new Set());
    setSinceDate('');
    setUntilDate('');
  };

  const handleSearchSelect = useCallback(
    (nodeId: string) => {
      if (!cyRef.current) return;
      const cy = cyRef.current;
      const node = cy.getElementById(nodeId);
      if (node.length === 0) return;

      cy.elements().unselect();
      cy.elements().removeClass('dimmed highlighted hovered');
      node.select();

      // Highlight its neighborhood.
      const neighbors = node.neighborhood('node');
      const connectedEdges = node.connectedEdges();
      cy.startBatch();
      cy.nodes().not(node).not(neighbors).addClass('dimmed');
      connectedEdges.addClass('highlighted');
      cy.edges().not(connectedEdges).addClass('dimmed');
      cy.endBatch();

      cy.animate({ center: { eles: node }, zoom: 1.2, duration: 400 });
      setSelectedNode({ id: nodeId, title: node.data('label') });
      setSearchOpen(false);
      setSearchQuery('');
    },
    [],
  );

  const handleFocusNode = useCallback((nodeId: string) => {
    setFocusNodeId(nodeId);
  }, []);

  const handleExitFocus = useCallback(() => {
    setFocusNodeId(null);
    setSelectedNode(null);
  }, []);

  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelectedSearchIdx((prev) =>
        Math.min(prev + 1, searchResults.length - 1),
      );
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelectedSearchIdx((prev) => Math.max(prev - 1, 0));
    } else if (e.key === 'Enter' && searchResults.length > 0) {
      e.preventDefault();
      handleSearchSelect(searchResults[selectedSearchIdx].id);
    } else if (e.key === 'Escape') {
      setSearchOpen(false);
      setSearchQuery('');
    }
  };

  // Keyboard navigation for graph nodes.
  const handleGraphKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!cyRef.current) return;
      const cy = cyRef.current;
      const visibleNodes = cy.nodes(':visible');
      if (visibleNodes.length === 0) return;

      if (e.key === 'Tab') {
        e.preventDefault();
        const nextIndex = e.shiftKey
          ? focusedNodeIndex <= 0
            ? visibleNodes.length - 1
            : focusedNodeIndex - 1
          : (focusedNodeIndex + 1) % visibleNodes.length;
        setFocusedNodeIndex(nextIndex);
        const node = visibleNodes[nextIndex];
        cy.elements().unselect();
        node.select();
        cy.animate({ center: { eles: node }, duration: 200 });
      } else if (
        e.key === 'Enter' &&
        focusedNodeIndex >= 0 &&
        focusedNodeIndex < visibleNodes.length
      ) {
        navigate(`/notes/${visibleNodes[focusedNodeIndex].id()}`);
      } else if (
        ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)
      ) {
        e.preventDefault();
        if (focusedNodeIndex < 0) {
          setFocusedNodeIndex(0);
          const node = visibleNodes[0];
          cy.elements().unselect();
          node.select();
          cy.animate({ center: { eles: node }, duration: 200 });
          return;
        }
        const currentNode = visibleNodes[focusedNodeIndex];
        const currentPos = currentNode.position();
        let bestNode: cytoscape.NodeSingular | null = null;
        let bestDist = Infinity;

        visibleNodes.forEach((node) => {
          if (node.id() === currentNode.id()) return;
          const pos = node.position();
          const dx = pos.x - currentPos.x;
          const dy = pos.y - currentPos.y;
          const dist = Math.sqrt(dx * dx + dy * dy);
          let isDirectional = false;
          if (e.key === 'ArrowRight' && dx > 0) isDirectional = true;
          if (e.key === 'ArrowLeft' && dx < 0) isDirectional = true;
          if (e.key === 'ArrowDown' && dy > 0) isDirectional = true;
          if (e.key === 'ArrowUp' && dy < 0) isDirectional = true;
          if (isDirectional && dist < bestDist) {
            bestDist = dist;
            bestNode = node;
          }
        });

        if (bestNode) {
          const targetId = (bestNode as cytoscape.NodeSingular).id();
          const idx = visibleNodes
            .toArray()
            .findIndex((n) => n.id() === targetId);
          setFocusedNodeIndex(idx);
          cy.elements().unselect();
          (bestNode as cytoscape.NodeSingular).select();
          cy.animate({ center: { eles: bestNode }, duration: 200 });
        }
      } else if (e.key === 'Escape') {
        setFocusedNodeIndex(-1);
        cy.elements().unselect();
        cy.elements().removeClass('dimmed highlighted hovered');
        setSelectedNode(null);
      }
    },
    [focusedNodeIndex, navigate],
  );

  // ----- Render -----

  if (loading && !graphData) {
    return (
      <div className={styles.container}>
        <GraphSkeleton />
      </div>
    );
  }

  if (error && !graphData) {
    return (
      <div className={styles.container}>
        <div className={styles.errorState}>
          <div className={styles.errorTitle}>Failed to load graph</div>
          <div className={styles.errorDescription}>{error}</div>
          <button className={styles.retryButton} onClick={fetchGraph}>
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (graphData && graphData.nodes.length === 0) {
    return (
      <div className={styles.container}>
        <div className={styles.emptyState}>
          <div className={styles.emptyTitle}>No notes yet</div>
          <div className={styles.emptyDescription}>
            Create some notes with [[wikilinks]] to see your knowledge graph
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.container}>
      {/* Accessibility instructions */}
      <div className={styles.srOnly}>
        Use Tab to focus graph nodes, Arrow keys to navigate between nodes,
        Enter to open a note, Escape to deselect, and / to search.
      </div>

      {/* Hull canvas overlay (behind graph visually, above in DOM with pointer-events:none) */}
      <canvas ref={hullCanvasRef} className={styles.hullCanvas} />

      {/* Main graph canvas */}
      <div
        ref={containerRef}
        className={styles.canvas}
        role="img"
        aria-label="Knowledge graph showing connections between notes"
        tabIndex={0}
        onKeyDown={handleGraphKeyDown}
      />

      {/* ---- Toolbar ---- */}
      <div className={styles.toolbar}>
        {/* Focus mode bar */}
        {focusNodeId && (
          <motion.div
            className={styles.focusBar}
            initial={{ opacity: 0, x: -12 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
          >
            <button className={styles.focusBackBtn} onClick={handleExitFocus}>
              <ArrowLeft size={14} />
              Back
            </button>
            <span className={styles.focusTitle}>
              Exploring:{' '}
              <span className={styles.focusTitleAccent}>
                {selectedNode?.title || 'Note'}
              </span>
            </span>
          </motion.div>
        )}

        {/* Node action bar (when a node is selected, not in focus mode) */}
        {selectedNode && !focusNodeId && (
          <motion.div
            className={styles.nodeActions}
            initial={{ opacity: 0, y: -8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.15, ease: [0.16, 1, 0.3, 1] }}
          >
            <span className={styles.nodeActionTitle}>
              {selectedNode.title}
            </span>
            <button
              className={styles.actionBtn}
              onClick={() => handleFocusNode(selectedNode.id)}
            >
              <Crosshair size={12} />
              Focus
            </button>
            <button
              className={styles.actionBtn}
              onClick={() => navigate(`/notes/${selectedNode.id}`)}
            >
              <ExternalLink size={12} />
              Open
            </button>
          </motion.div>
        )}

        <div className={styles.toolbarRight}>
          {/* Search */}
          <div className={styles.searchContainer}>
            <AnimatePresence mode="wait">
              {searchOpen ? (
                <motion.div
                  key="search-input"
                  className={styles.searchInputWrapper}
                  initial={{ opacity: 0, width: 32 }}
                  animate={{ opacity: 1, width: 'auto' }}
                  exit={{ opacity: 0, width: 32 }}
                  transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
                >
                  <Search size={14} />
                  <input
                    ref={searchInputRef}
                    className={styles.searchInput}
                    type="text"
                    placeholder="Find a note..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    onKeyDown={handleSearchKeyDown}
                  />
                  <button
                    className={styles.searchClose}
                    onClick={() => {
                      setSearchOpen(false);
                      setSearchQuery('');
                    }}
                  >
                    <X size={14} />
                  </button>
                </motion.div>
              ) : (
                <motion.button
                  key="search-toggle"
                  className={styles.iconBtn}
                  onClick={() => {
                    setSearchOpen(true);
                    setTimeout(() => searchInputRef.current?.focus(), 0);
                  }}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  title="Search nodes (/)"
                >
                  <Search size={16} />
                </motion.button>
              )}
            </AnimatePresence>

            {/* Search dropdown */}
            {searchOpen && searchQuery.trim() && (
              <div className={styles.searchDropdown}>
                {searchResults.length === 0 ? (
                  <div className={styles.searchEmpty}>No matching notes</div>
                ) : (
                  searchResults.map((result, idx) => {
                    const color = result.project_id
                      ? projectColorMap().get(result.project_id)
                      : undefined;
                    return (
                      <div
                        key={result.id}
                        className={`${styles.searchResult} ${idx === selectedSearchIdx ? styles.searchResultActive : ''}`}
                        onClick={() => handleSearchSelect(result.id)}
                        onMouseEnter={() => setSelectedSearchIdx(idx)}
                      >
                        <span className={styles.searchResultTitle}>
                          {result.title}
                        </span>
                        <span className={styles.searchResultMeta}>
                          {color && (
                            <span
                              className={styles.searchResultDot}
                              style={{ backgroundColor: color }}
                            />
                          )}
                          {result.project || ''}
                          {result.project && result.tags.length > 0 && ' · '}
                          {result.tags.slice(0, 2).map((t) => `#${t}`).join(' ')}
                        </span>
                      </div>
                    );
                  })
                )}
              </div>
            )}
          </div>

          {/* Filter toggle */}
          <button
            className={`${styles.iconBtn} ${filterOpen ? styles.iconBtnActive : ''}`}
            onClick={() => setFilterOpen(!filterOpen)}
            title="Toggle filters"
          >
            <Filter size={16} />
            {activeFilterCount > 0 && (
              <span className={styles.filterBadge} />
            )}
          </button>
        </div>
      </div>

      {/* ---- Filter panel (collapsible) ---- */}
      <AnimatePresence>
        {filterOpen && (
          <motion.div
            className={styles.filterPanel}
            initial={{ opacity: 0, x: -12 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -12 }}
            transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
          >
            <div className={styles.filterTitle}>Filters</div>

            {projects.length > 0 && (
              <div className={styles.filterSection}>
                <div className={styles.filterSectionLabel}>Projects</div>
                {projects.map((project, index) => (
                  <label key={project.id} className={styles.checkboxRow}>
                    <input
                      type="checkbox"
                      checked={selectedProjects.has(project.id)}
                      onChange={() => handleProjectToggle(project.id)}
                    />
                    <span
                      className={styles.projectDot}
                      style={{ backgroundColor: getProjectColor(index) }}
                    />
                    {project.name}
                  </label>
                ))}
              </div>
            )}

            {tags.length > 0 && (
              <div className={styles.filterSection}>
                <div className={styles.filterSectionLabel}>Tags</div>
                <div className={styles.tagPills}>
                  {tags.slice(0, 15).map((tag) => (
                    <button
                      key={tag.name}
                      className={`${styles.tagPill} ${activeTags.has(tag.name) ? styles.tagPillActive : ''}`}
                      onClick={() => handleTagToggle(tag.name)}
                    >
                      #{tag.name}
                    </button>
                  ))}
                </div>
              </div>
            )}

            <div className={styles.filterSection}>
              <div className={styles.filterSectionLabel}>Date range</div>
              <div className={styles.dateInputs}>
                <input
                  type="date"
                  className={styles.dateInput}
                  value={sinceDate}
                  onChange={(e) => setSinceDate(e.target.value)}
                  aria-label="Since date"
                  placeholder="Since"
                />
                <input
                  type="date"
                  className={styles.dateInput}
                  value={untilDate}
                  onChange={(e) => setUntilDate(e.target.value)}
                  aria-label="Until date"
                  placeholder="Until"
                />
              </div>
            </div>

            <button className={styles.resetButton} onClick={handleReset}>
              Reset filters
            </button>
          </motion.div>
        )}
      </AnimatePresence>

      {/* ---- Rich tooltip ---- */}
      {tooltip && (
        <div
          className={styles.richTooltip}
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          <div className={styles.tooltipTitle}>{tooltip.title}</div>
          {tooltip.project && (
            <div className={styles.tooltipProject}>
              <span
                className={styles.tooltipProjectDot}
                style={{ backgroundColor: tooltip.projectColor }}
              />
              {tooltip.project}
            </div>
          )}
          {tooltip.tags.length > 0 && (
            <div className={styles.tooltipTags}>
              {tooltip.tags.slice(0, 4).map((t) => (
                <span key={t} className={styles.tooltipTag}>
                  #{t}
                </span>
              ))}
            </div>
          )}
          <div className={styles.tooltipMeta}>
            {tooltip.linkCount} {tooltip.linkCount === 1 ? 'link' : 'links'}
            {tooltip.date && ` · ${tooltip.date}`}
          </div>
        </div>
      )}

      {/* ---- Minimap ---- */}
      <div ref={minimapRef} className={styles.minimap} />

      {/* ---- Stats bar ---- */}
      {displayData && (
        <div className={styles.statsBar}>
          <span>
            {visibleCounts.nodes} {visibleCounts.nodes === 1 ? 'note' : 'notes'}
          </span>
          <span className={styles.statsDot} />
          <span>
            {visibleCounts.edges} {visibleCounts.edges === 1 ? 'link' : 'links'}
          </span>
          {visibleCounts.projects > 0 && (
            <>
              <span className={styles.statsDot} />
              <span>
                {visibleCounts.projects}{' '}
                {visibleCounts.projects === 1 ? 'project' : 'projects'}
              </span>
            </>
          )}
          {orphanCount > 0 && (
            <>
              <span className={styles.statsDot} />
              <button
                className={styles.orphanBtn}
                title="Notes with no links"
                onClick={() => {
                  addToast(
                    `${orphanCount} orphan note${orphanCount === 1 ? '' : 's'} with no links`,
                    'info',
                  );
                }}
              >
                <Unlink size={10} />
                {orphanCount} orphan{orphanCount === 1 ? '' : 's'}
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
