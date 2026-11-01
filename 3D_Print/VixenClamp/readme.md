# Vixen Clamp - 3D Print Files

This folder contains 3D printable mounting parts for attaching the SV241 controller to a Vixen-style dovetail bar.

## Files

| File | Description |
|------|-------------|
| `vixen_clamp_a.3mf` | Main clamp part (ready-to-print) |
| `vixen_clamp_a.FCStd` | FreeCAD source file for vixen_clamp_a |
| `vixen_clamp_b.3mf` | Second clamp part (ready-to-print) |
| `vixen_clamp_b.FCStd` | FreeCAD source file for vixen_clamp_b |
| `vixen_clamp.sc` | SolidCraft source file with both parts (A and B) as separate bodies, side by side as for printing |

## Required Hardware

| Qty | Part |
|-----|------|
| 1x | DIN 912 M5x40mm screw |
| 1x | DIN 557 M5 square nut |
| 1x | DIN 125 M5 washer (5.3mm) |
| 2x | DIN 912 M4x10mm screws |

## Assembly

Both parts (A and B) are needed for a complete clamp assembly.

## Editing

The `.FCStd` files can be opened and modified in [FreeCAD](https://www.freecad.org/) (free and open-source).

The `.sc` file can be opened and modified in SolidCraft. It holds both parts as the bodies `Klemme_A` and `Klemme_B`, fully parametric: the parameters of part A start with `A_` (for example `A_Laenge`), those of part B with `B_` (for example `B_Hoehe`). Part B is placed `B_Abstand` (10 mm) next to part A. The `.sc` file was rebuilt from the FreeCAD files and matches their volumes to within 0.3 mm³; the `.3mf` files were not regenerated from it.
