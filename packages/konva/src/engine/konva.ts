// The Konva the renderer builds nodes from: the core (Stage, Layer, FastLayer,
// Group, Shape) plus every shape react-konva exposes. Each shape module
// registers its class on Konva, so `Konva[type]` resolves it. The filters of
// the full `konva` entry are left out; an app that uses them imports them
// (or 'konva') itself and shares these modules.

import Konva from 'konva/lib/Core.js';
import 'konva/lib/shapes/Arc.js';
import 'konva/lib/shapes/Arrow.js';
import 'konva/lib/shapes/Circle.js';
import 'konva/lib/shapes/Ellipse.js';
import 'konva/lib/shapes/Image.js';
import 'konva/lib/shapes/Label.js';
import 'konva/lib/shapes/Line.js';
import 'konva/lib/shapes/Path.js';
import 'konva/lib/shapes/Rect.js';
import 'konva/lib/shapes/RegularPolygon.js';
import 'konva/lib/shapes/Ring.js';
import 'konva/lib/shapes/Sprite.js';
import 'konva/lib/shapes/Star.js';
import 'konva/lib/shapes/Text.js';
import 'konva/lib/shapes/TextPath.js';
import 'konva/lib/shapes/Transformer.js';
import 'konva/lib/shapes/Wedge.js';

export { Konva };
