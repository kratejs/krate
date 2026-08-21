declare global {
  namespace JSX {
    interface Element extends Node {}
    interface ElementChildrenAttribute { children: {} }

    type Booleanish = boolean | 'true' | 'false';
    type CSSProperties = Record<string, string | number>;

    interface AriaAttributes {
      'aria-activedescendant'?: string;
      'aria-atomic'?: Booleanish;
      'aria-autocomplete'?: 'none' | 'inline' | 'list' | 'both';
      'aria-busy'?: Booleanish;
      'aria-checked'?: boolean | 'false' | 'mixed' | 'true';
      'aria-colcount'?: number;
      'aria-colindex'?: number;
      'aria-colspan'?: number;
      'aria-controls'?: string;
      'aria-current'?: boolean | 'false' | 'true' | 'page' | 'step' | 'location' | 'date' | 'time';
      'aria-describedby'?: string;
      'aria-details'?: string;
      'aria-disabled'?: Booleanish;
      'aria-dropeffect'?: 'none' | 'copy' | 'execute' | 'link' | 'move' | 'popup';
      'aria-errormessage'?: string;
      'aria-expanded'?: Booleanish;
      'aria-flowto'?: string;
      'aria-grabbed'?: Booleanish;
      'aria-haspopup'?: boolean | 'false' | 'true' | 'menu' | 'listbox' | 'tree' | 'grid' | 'dialog';
      'aria-hidden'?: Booleanish;
      'aria-invalid'?: boolean | 'false' | 'true' | 'grammar' | 'spelling';
      'aria-keyshortcuts'?: string;
      'aria-label'?: string;
      'aria-labelledby'?: string;
      'aria-level'?: number;
      'aria-live'?: 'off' | 'assertive' | 'polite';
      'aria-modal'?: Booleanish;
      'aria-multiline'?: Booleanish;
      'aria-multiselectable'?: Booleanish;
      'aria-orientation'?: 'horizontal' | 'vertical';
      'aria-owns'?: string;
      'aria-placeholder'?: string;
      'aria-posinset'?: number;
      'aria-pressed'?: boolean | 'false' | 'mixed' | 'true';
      'aria-readonly'?: Booleanish;
      'aria-relevant'?: 'additions' | 'additions text' | 'all' | 'removals' | 'text';
      'aria-required'?: Booleanish;
      'aria-roledescription'?: string;
      'aria-rowcount'?: number;
      'aria-rowindex'?: number;
      'aria-rowspan'?: number;
      'aria-selected'?: Booleanish;
      'aria-setsize'?: number;
      'aria-sort'?: 'none' | 'ascending' | 'descending' | 'other';
      'aria-valuemax'?: number;
      'aria-valuemin'?: number;
      'aria-valuenow'?: number;
      'aria-valuetext'?: string;
    }

    interface EventHandler<E extends Event = Event> { (event: E): void }

    interface ClipboardEventHandler {
      onCopy?: EventHandler<ClipboardEvent>;
      onCut?: EventHandler<ClipboardEvent>;
      onPaste?: EventHandler<ClipboardEvent>;
    }

    interface CompositionEventHandler {
      onCompositionEnd?: EventHandler<CompositionEvent>;
      onCompositionStart?: EventHandler<CompositionEvent>;
      onCompositionUpdate?: EventHandler<CompositionEvent>;
    }

    interface FocusEventHandler {
      onFocus?: EventHandler<FocusEvent>;
      onBlur?: EventHandler<FocusEvent>;
    }

    interface FormEventHandler {
      onChange?: EventHandler<Event>;
      onInput?: EventHandler<Event>;
      onSubmit?: EventHandler<Event>;
      onReset?: EventHandler<Event>;
      onInvalid?: EventHandler<Event>;
    }

    interface KeyboardEventHandler {
      onKeyDown?: EventHandler<KeyboardEvent>;
      onKeyUp?: EventHandler<KeyboardEvent>;
      onKeyPress?: EventHandler<KeyboardEvent>;
    }

    interface MouseEventHandler {
      onClick?: EventHandler<MouseEvent>;
      onContextMenu?: EventHandler<MouseEvent>;
      onDoubleClick?: EventHandler<MouseEvent>;
      onDrag?: EventHandler<DragEvent>;
      onDragEnd?: EventHandler<DragEvent>;
      onDragEnter?: EventHandler<DragEvent>;
      onDragLeave?: EventHandler<DragEvent>;
      onDragOver?: EventHandler<DragEvent>;
      onDragStart?: EventHandler<DragEvent>;
      onDrop?: EventHandler<DragEvent>;
      onMouseDown?: EventHandler<MouseEvent>;
      onMouseEnter?: EventHandler<MouseEvent>;
      onMouseLeave?: EventHandler<MouseEvent>;
      onMouseMove?: EventHandler<MouseEvent>;
      onMouseOut?: EventHandler<MouseEvent>;
      onMouseOver?: EventHandler<MouseEvent>;
      onMouseUp?: EventHandler<MouseEvent>;
    }

    interface PointerEventHandler {
      onPointerDown?: EventHandler<PointerEvent>;
      onPointerMove?: EventHandler<PointerEvent>;
      onPointerUp?: EventHandler<PointerEvent>;
      onPointerCancel?: EventHandler<PointerEvent>;
      onPointerEnter?: EventHandler<PointerEvent>;
      onPointerLeave?: EventHandler<PointerEvent>;
      onPointerOver?: EventHandler<PointerEvent>;
      onPointerOut?: EventHandler<PointerEvent>;
    }

    interface TouchEventHandler {
      onTouchCancel?: EventHandler<TouchEvent>;
      onTouchEnd?: EventHandler<TouchEvent>;
      onTouchMove?: EventHandler<TouchEvent>;
      onTouchStart?: EventHandler<TouchEvent>;
    }

    interface WheelEventHandler {
      onWheel?: EventHandler<WheelEvent>;
    }

    interface AnimationEventHandler {
      onAnimationStart?: EventHandler<AnimationEvent>;
      onAnimationEnd?: EventHandler<AnimationEvent>;
      onAnimationIteration?: EventHandler<AnimationEvent>;
    }

    interface TransitionEventHandler {
      onTransitionEnd?: EventHandler<TransitionEvent>;
    }

    interface UIEventHandler {
      onScroll?: EventHandler<Event>;
      onResize?: EventHandler<UIEvent>;
    }

    interface MediaEventHandler {
      onAbort?: EventHandler<Event>;
      onCanPlay?: EventHandler<Event>;
      onCanPlayThrough?: EventHandler<Event>;
      onDurationChange?: EventHandler<Event>;
      onEmptied?: EventHandler<Event>;
      onEncrypted?: EventHandler<Event>;
      onEnded?: EventHandler<Event>;
      onError?: EventHandler<Event>;
      onLoadedData?: EventHandler<Event>;
      onLoadedMetadata?: EventHandler<Event>;
      onLoadStart?: EventHandler<Event>;
      onPause?: EventHandler<Event>;
      onPlay?: EventHandler<Event>;
      onPlaying?: EventHandler<Event>;
      onProgress?: EventHandler<Event>;
      onRateChange?: EventHandler<Event>;
      onSeeked?: EventHandler<Event>;
      onSeeking?: EventHandler<Event>;
      onStalled?: EventHandler<Event>;
      onSuspend?: EventHandler<Event>;
      onTimeUpdate?: EventHandler<Event>;
      onVolumeChange?: EventHandler<Event>;
      onWaiting?: EventHandler<Event>;
    }

    interface DOMAttributes {
      children?: unknown[] | unknown;
      key?: string | number;
      ref?: (el: Element) => void;
      slot?: string;
      style?: CSSProperties | string;
      part?: string;
      exportparts?: string;
    }

    interface HTMLAttributes extends AriaAttributes, ClipboardEventHandler, CompositionEventHandler, FocusEventHandler, FormEventHandler, KeyboardEventHandler, MouseEventHandler, PointerEventHandler, TouchEventHandler, WheelEventHandler, AnimationEventHandler, TransitionEventHandler, UIEventHandler, MediaEventHandler, DOMAttributes {
      accessKey?: string;
      class?: string;
      className?: string;
      contentEditable?: Booleanish | 'inherit' | 'false' | 'true';
      contextMenu?: string;
      dir?: string;
      draggable?: Booleanish;
      hidden?: boolean | string;
      id?: string;
      inert?: boolean | string;
      lang?: string;
      nonce?: string;
      role?: string;
      spellcheck?: Booleanish;
      tabIndex?: number;
      title?: string;
      translate?: 'yes' | 'no';
      about?: string;
      datatype?: string;
      inlist?: unknown;
      prefix?: string;
      property?: string;
      resource?: string;
      typeof?: string;
      vocab?: string;
      autoCapitalize?: string;
      autoCorrect?: string;
      autoSave?: string;
      color?: string;
      itemprop?: string;
      itemScope?: boolean;
      itemType?: string;
      itemID?: string;
      itemRef?: string;
      results?: number;
      security?: string;
      unselectable?: 'on' | 'off';
      is?: string;
      popover?: 'auto' | 'manual' | boolean;
      popovertarget?: string;
      popovertargetaction?: 'toggle' | 'show' | 'hide';
      writingSuggestions?: 'mark' | 'suggestion';
      inputMode?: 'none' | 'text' | 'tel' | 'url' | 'email' | 'numeric' | 'decimal' | 'search';
      placeholder?: string;
    }

    interface AnchorHTMLAttributes extends HTMLAttributes {
      download?: string;
      href?: string;
      hrefLang?: string;
      media?: string;
      ping?: string;
      referrerPolicy?: 'no-referrer' | 'no-referrer-when-downgrade' | 'origin' | 'origin-when-cross-origin' | 'same-origin' | 'strict-origin' | 'strict-origin-when-cross-origin' | 'unsafe-url';
      rel?: string;
      target?: string;
      type?: string;
    }

    interface AreaHTMLAttributes extends HTMLAttributes {
      alt?: string;
      coords?: string;
      download?: string;
      href?: string;
      hrefLang?: string;
      media?: string;
      referrerPolicy?: string;
      rel?: string;
      shape?: string;
      target?: string;
    }

    interface AudioHTMLAttributes extends MediaHTMLAttributes {
      crossOrigin?: string;
      loop?: boolean | string;
      muted?: boolean | string;
      preload?: string;
      src?: string;
    }

    interface BaseHTMLAttributes extends HTMLAttributes {
      href?: string;
      target?: string;
    }

    interface BlockquoteHTMLAttributes extends HTMLAttributes { cite?: string }

    interface CanvasHTMLAttributes extends HTMLAttributes {
      height?: number | string;
      width?: number | string;
    }

    interface ColHTMLAttributes extends HTMLAttributes {
      span?: number;
      width?: number | string;
    }

    interface ColgroupHTMLAttributes extends HTMLAttributes { span?: number }

    interface DataHTMLAttributes extends HTMLAttributes { value?: string | number }

    interface DelHTMLAttributes extends HTMLAttributes {
      cite?: string;
      dateTime?: string;
    }

    interface DetailsHTMLAttributes extends HTMLAttributes {
      open?: boolean | string;
      name?: string;
    }

    interface DialogHTMLAttributes extends HTMLAttributes { open?: boolean | string }

    interface EmbedHTMLAttributes extends HTMLAttributes {
      height?: number | string;
      src?: string;
      type?: string;
      width?: number | string;
    }

    interface FieldsetHTMLAttributes extends HTMLAttributes {
      disabled?: boolean | string;
      form?: string;
      name?: string;
    }

    interface HtmlHTMLAttributes extends HTMLAttributes { manifest?: string }

    interface IframeHTMLAttributes extends HTMLAttributes {
      allow?: string;
      allowFullScreen?: boolean;
      allowTransparency?: boolean;
      creditionals?: string;
      height?: number | string;
      loading?: 'eager' | 'lazy';
      name?: string;
      referrerPolicy?: string;
      sandbox?: string;
      seamless?: boolean;
      src?: string;
      srcDoc?: string;
      width?: number | string;
    }

    interface ImgHTMLAttributes extends HTMLAttributes {
      alt?: string;
      crossOrigin?: string;
      decoding?: 'async' | 'auto' | 'sync';
      height?: number | string;
      loading?: 'eager' | 'lazy';
      referrerPolicy?: string;
      sizes?: string;
      src?: string;
      srcSet?: string;
      useMap?: string;
      width?: number | string;
      fetchPriority?: 'high' | 'low' | 'auto';
    }

    interface InputHTMLAttributes extends Omit<HTMLAttributes, 'onChange' | 'onInput'> {
      accept?: string;
      alt?: string;
      autoComplete?: string;
      autoFocus?: boolean;
      capture?: boolean | string;
      checked?: boolean | string;
      crossOrigin?: string;
      disabled?: boolean | string;
      form?: string;
      formAction?: string;
      formEncType?: string;
      formMethod?: string;
      formNoValidate?: boolean;
      formTarget?: string;
      height?: number | string;
      list?: string;
      max?: number | string;
      maxLength?: number;
      min?: number | string;
      minLength?: number;
      multiple?: boolean;
      name?: string;
      pattern?: string;
      placeholder?: string;
      readOnly?: boolean;
      required?: boolean | string;
      size?: number;
      src?: string;
      step?: number | string;
      type?: string;
      value?: string | number;
      width?: number | string;
      onChange?: (event: Event & { target: HTMLInputElement; currentTarget: HTMLInputElement }) => void;
      onInput?: (event: Event & { target: HTMLInputElement; currentTarget: HTMLInputElement }) => void;
    }

    interface SelectHTMLAttributes extends Omit<HTMLAttributes, 'onChange'> {
      autoComplete?: string;
      autoFocus?: boolean;
      disabled?: boolean | string;
      form?: string;
      multiple?: boolean;
      name?: string;
      required?: boolean | string;
      size?: number;
      value?: string | number;
      onChange?: (event: Event & { target: HTMLSelectElement; currentTarget: HTMLSelectElement }) => void;
    }

    interface TextareaHTMLAttributes extends Omit<HTMLAttributes, 'onChange' | 'onInput'> {
      autoComplete?: string;
      autoFocus?: boolean;
      cols?: number;
      dirName?: string;
      disabled?: boolean | string;
      form?: string;
      maxLength?: number;
      minLength?: number;
      name?: string;
      placeholder?: string;
      readOnly?: boolean;
      required?: boolean | string;
      rows?: number;
      value?: string | number;
      wrap?: string;
      onChange?: (event: Event & { target: HTMLTextAreaElement; currentTarget: HTMLTextAreaElement }) => void;
      onInput?: (event: Event & { target: HTMLTextAreaElement; currentTarget: HTMLTextAreaElement }) => void;
    }

    interface ButtonHTMLAttributes extends Omit<HTMLAttributes, 'onClick'> {
      autoFocus?: boolean;
      disabled?: boolean | string;
      form?: string;
      formAction?: string;
      formEncType?: string;
      formMethod?: string;
      formNoValidate?: boolean;
      formTarget?: string;
      name?: string;
      popovertarget?: string;
      popovertargetaction?: 'toggle' | 'show' | 'hide';
      type?: 'submit' | 'reset' | 'button';
      value?: string;
      onClick?: (event: MouseEvent & { target: HTMLButtonElement; currentTarget: HTMLButtonElement }) => void;
    }

    interface FormHTMLAttributes extends Omit<HTMLAttributes, 'onSubmit' | 'onReset'> {
      acceptCharset?: string;
      action?: string;
      autoComplete?: string;
      encType?: string;
      method?: string;
      name?: string;
      noValidate?: boolean;
      rel?: string;
      target?: string;
      onSubmit?: (event: Event & { target: HTMLFormElement; currentTarget: HTMLFormElement }) => void;
      onReset?: (event: Event & { target: HTMLFormElement; currentTarget: HTMLFormElement }) => void;
    }

    interface InsHTMLAttributes extends HTMLAttributes {
      cite?: string;
      dateTime?: string;
    }

    interface LabelHTMLAttributes extends HTMLAttributes {
      for?: string;
      htmlFor?: string;
      form?: string;
    }

    interface LiHTMLAttributes extends HTMLAttributes {
      value?: string | number;
      type?: string;
    }

    interface LinkHTMLAttributes extends HTMLAttributes {
      as?: string;
      crossOrigin?: string;
      href?: string;
      hrefLang?: string;
      integrity?: string;
      media?: string;
      referrerPolicy?: string;
      rel?: string;
      sizes?: string;
      type?: string;
      fetchPriority?: 'high' | 'low' | 'auto';
    }

    interface MapHTMLAttributes extends HTMLAttributes { name?: string }

    interface MediaHTMLAttributes extends HTMLAttributes {
      autoPlay?: boolean;
      controls?: boolean | string;
      controlsList?: string;
      crossOrigin?: string;
      loop?: boolean | string;
      mediaGroup?: string;
      muted?: boolean | string;
      playsInline?: boolean;
      preload?: string;
      src?: string;
    }

    interface MenuHTMLAttributes extends HTMLAttributes { type?: string }

    interface MetaHTMLAttributes extends HTMLAttributes {
      charSet?: string;
      content?: string;
      httpEquiv?: string;
      media?: string;
      name?: string;
    }

    interface MeterHTMLAttributes extends HTMLAttributes {
      form?: string;
      high?: number;
      low?: number;
      max?: number | string;
      min?: number | string;
      optimum?: number;
      value?: string | number;
    }

    interface ObjectHTMLAttributes extends HTMLAttributes {
      classID?: string;
      data?: string;
      form?: string;
      height?: number | string;
      name?: string;
      type?: string;
      useMap?: string;
      width?: number | string;
    }

    interface OlHTMLAttributes extends HTMLAttributes {
      reversed?: boolean;
      start?: number;
      type?: '1' | 'A' | 'a' | 'I' | 'i';
    }

    interface OptgroupHTMLAttributes extends HTMLAttributes {
      disabled?: boolean | string;
      label?: string;
    }

    interface OptionHTMLAttributes extends HTMLAttributes {
      disabled?: boolean | string;
      label?: string;
      selected?: boolean | string;
      value?: string | number;
    }

    interface OutputHTMLAttributes extends HTMLAttributes {
      for?: string;
      form?: string;
      name?: string;
    }

    interface ProgressHTMLAttributes extends HTMLAttributes {
      max?: number | string;
      value?: string | number;
    }

    interface QuoteHTMLAttributes extends HTMLAttributes { cite?: string }

    interface ScriptHTMLAttributes extends HTMLAttributes {
      async?: boolean;
      crossOrigin?: string;
      defer?: boolean;
      integrity?: string;
      noModule?: boolean;
      referrerPolicy?: string;
      src?: string;
      type?: string;
    }

    interface SlotHTMLAttributes extends HTMLAttributes { name?: string }

    interface SourceHTMLAttributes extends HTMLAttributes {
      height?: number | string;
      media?: string;
      sizes?: string;
      src?: string;
      srcSet?: string;
      type?: string;
      width?: number | string;
    }

    interface StyleHTMLAttributes extends HTMLAttributes {
      media?: string;
      scoped?: boolean;
      type?: string;
    }

    interface TableHTMLAttributes extends HTMLAttributes {
      cellPadding?: number | string;
      cellSpacing?: number | string;
      summary?: string;
      width?: number | string;
    }

    interface TdHTMLAttributes extends HTMLAttributes {
      colSpan?: number;
      headers?: string;
      rowSpan?: number;
      scope?: string;
      abbr?: string;
      width?: number | string;
    }

    interface ThHTMLAttributes extends HTMLAttributes {
      abbr?: string;
      colSpan?: number;
      headers?: string;
      rowSpan?: number;
      scope?: 'col' | 'colgroup' | 'row' | 'rowgroup';
      width?: number | string;
    }

    interface TimeHTMLAttributes extends HTMLAttributes { dateTime?: string }

    interface TrackHTMLAttributes extends HTMLAttributes {
      default?: boolean;
      kind?: string;
      label?: string;
      src?: string;
      srcLang?: string;
    }

    interface VideoHTMLAttributes extends MediaHTMLAttributes {
      height?: number | string;
      playsInline?: boolean;
      poster?: string;
      width?: number | string;
      disablePictureInPicture?: boolean;
    }

    interface SVGAttributes extends AriaAttributes {
      children?: unknown[] | unknown;
      key?: string | number;
      ref?: (el: Element) => void;
      style?: CSSProperties | string;
      class?: string;
      className?: string;
      id?: string;
      lang?: string;
      role?: string;
      tabIndex?: number;
      title?: string;
      accentHeight?: number | string;
      accumulate?: 'none' | 'sum';
      additive?: 'replace' | 'sum';
      alignmentBaseline?: 'auto' | 'baseline' | 'before-edge' | 'text-before-edge' | 'middle' | 'central' | 'after-edge' | 'text-after-edge' | 'ideographic' | 'alphabetic' | 'hanging' | 'mathematical' | 'inherit';
      allowReorder?: 'no' | 'yes';
      alphabetic?: number | string;
      amplitude?: number | string;
      arabicForm?: 'initial' | 'medial' | 'terminal' | 'isolated';
      ascent?: number | string;
      attributeName?: string;
      attributeType?: string;
      autoReverse?: Booleanish;
      azimuth?: number | string;
      baseFrequency?: number | string;
      baselineShift?: number | string;
      baseProfile?: number | string;
      bbox?: number | string;
      begin?: number | string;
      bias?: number | string;
      by?: number | string;
      calcMode?: number | string;
      capHeight?: number | string;
      clip?: number | string;
      clipPath?: string;
      clipPathUnits?: number | string;
      clipRule?: number | string;
      colorInterpolation?: number | string;
      colorInterpolationFilters?: 'auto' | 'sRGB' | 'linearRGB' | 'inherit';
      colorProfile?: number | string;
      colorRendering?: number | string;
      contentScriptType?: number | string;
      contentStyleType?: number | string;
      cursor?: number | string;
      cx?: number | string;
      cy?: number | string;
      d?: string;
      decelerate?: number | string;
      descent?: number | string;
      diffuseConstant?: number | string;
      direction?: number | string;
      display?: number | string;
      divisor?: number | string;
      dominantBaseline?: number | string;
      dur?: number | string;
      dx?: number | string;
      dy?: number | string;
      edgeMode?: number | string;
      elevation?: number | string;
      enableBackground?: number | string;
      end?: number | string;
      exponent?: number | string;
      externalResourcesRequired?: Booleanish;
      fill?: string;
      fillOpacity?: number | string;
      fillRule?: 'nonzero' | 'evenodd' | 'inherit';
      filter?: string;
      filterRes?: number | string;
      filterUnits?: number | string;
      floodColor?: number | string;
      floodOpacity?: number | string;
      focusable?: Booleanish | 'auto';
      fontFamily?: string;
      fontSize?: number | string;
      fontSizeAdjust?: number | string;
      fontStretch?: number | string;
      fontStyle?: number | string;
      fontVariant?: number | string;
      fontWeight?: number | string;
      format?: number | string;
      fr?: number | string;
      from?: number | string;
      fx?: number | string;
      fy?: number | string;
      g1?: number | string;
      g2?: number | string;
      glyphName?: number | string;
      glyphOrientationHorizontal?: number | string;
      glyphOrientationVertical?: number | string;
      glyphRef?: number | string;
      gradientTransform?: string;
      gradientUnits?: string;
      hanging?: number | string;
      height?: number | string;
      horizAdvX?: number | string;
      horizOriginX?: number | string;
      href?: string;
      ideographic?: number | string;
      imageRendering?: number | string;
      in2?: number | string;
      in?: string;
      intercept?: number | string;
      k1?: number | string;
      k2?: number | string;
      k3?: number | string;
      k4?: number | string;
      k?: number | string;
      kernelMatrix?: number | string;
      kernelUnitLength?: number | string;
      kerning?: number | string;
      keyPoints?: number | string;
      keySplines?: number | string;
      keyTimes?: number | string;
      lengthAdjust?: number | string;
      letterSpacing?: number | string;
      lightingColor?: number | string;
      limitingConeAngle?: number | string;
      local?: number | string;
      markerEnd?: string;
      markerHeight?: number | string;
      markerMid?: string;
      markerStart?: string;
      markerUnits?: number | string;
      markerWidth?: number | string;
      mask?: string;
      maskContentUnits?: number | string;
      maskUnits?: number | string;
      mathematical?: number | string;
      max?: number | string;
      media?: string;
      method?: string;
      min?: number | string;
      mode?: number | string;
      name?: string;
      numOctaves?: number | string;
      offset?: number | string;
      opacity?: number | string;
      operator?: number | string;
      order?: number | string;
      orient?: number | string;
      orientation?: number | string;
      origin?: number | string;
      overflow?: number | string;
      overlinePosition?: number | string;
      overlineThickness?: number | string;
      paintOrder?: number | string;
      panose1?: number | string;
      path?: string;
      pathLength?: number | string;
      patternContentUnits?: string;
      patternTransform?: number | string;
      patternUnits?: string;
      pointerEvents?: number | string;
      points?: string;
      pointsAtX?: number | string;
      pointsAtY?: number | string;
      pointsAtZ?: number | string;
      preserveAlpha?: Booleanish;
      preserveAspectRatio?: string;
      primitiveUnits?: number | string;
      r?: number | string;
      radius?: number | string;
      refX?: number | string;
      refY?: number | string;
      renderingIntent?: number | string;
      repeatCount?: number | string;
      repeatDur?: number | string;
      requiredExtensions?: number | string;
      requiredFeatures?: number | string;
      restart?: number | string;
      result?: string;
      rotate?: number | string;
      rx?: number | string;
      ry?: number | string;
      scale?: number | string;
      seed?: number | string;
      shapeRendering?: number | string;
      slope?: number | string;
      spacing?: number | string;
      specularConstant?: number | string;
      specularExponent?: number | string;
      speed?: number | string;
      spreadMethod?: string;
      startOffset?: number | string;
      stdDeviation?: number | string;
      stemh?: number | string;
      stemv?: number | string;
      stitchTiles?: number | string;
      stopColor?: string;
      stopOpacity?: number | string;
      strikethroughPosition?: number | string;
      strikethroughThickness?: number | string;
      string?: number | string;
      stroke?: string;
      strokeDasharray?: string | number;
      strokeDashoffset?: string | number;
      strokeLinecap?: 'butt' | 'round' | 'square' | 'inherit';
      strokeLinejoin?: 'miter' | 'round' | 'bevel' | 'inherit';
      strokeMiterlimit?: number | string;
      strokeOpacity?: number | string;
      strokeWidth?: number | string;
      surfaceScale?: number | string;
      systemLanguage?: number | string;
      tableValues?: number | string;
      targetX?: number | string;
      targetY?: number | string;
      textAnchor?: string;
      textDecoration?: number | string;
      textLength?: number | string;
      textRendering?: number | string;
      to?: number | string;
      transform?: string;
      u1?: number | string;
      u2?: number | string;
      underlinePosition?: number | string;
      underlineThickness?: number | string;
      unicode?: number | string;
      unicodeBidi?: number | string;
      unicodeRange?: number | string;
      unitsPerEm?: number | string;
      vAlphabetic?: number | string;
      vHanging?: number | string;
      vIdeographic?: number | string;
      vMathematical?: number | string;
      values?: string;
      vectorEffect?: number | string;
      version?: string;
      vertAdvY?: number | string;
      vertOriginX?: number | string;
      vertOriginY?: number | string;
      viewBox?: string;
      viewTarget?: number | string;
      visibility?: number | string;
      width?: number | string;
      widths?: number | string;
      wordSpacing?: number | string;
      writingMode?: number | string;
      x1?: number | string;
      x2?: number | string;
      x?: number | string;
      xChannelSelector?: string;
      xHeight?: number | string;
      y1?: number | string;
      y2?: number | string;
      y?: number | string;
      yChannelSelector?: string;
      z?: number | string;
      zoomAndPan?: string;
    }

    interface IntrinsicElements {
      a: AnchorHTMLAttributes;
      abbr: HTMLAttributes;
      address: HTMLAttributes;
      area: AreaHTMLAttributes;
      article: HTMLAttributes;
      aside: HTMLAttributes;
      audio: AudioHTMLAttributes;
      b: HTMLAttributes;
      base: BaseHTMLAttributes;
      bdi: HTMLAttributes;
      bdo: HTMLAttributes;
      blockquote: BlockquoteHTMLAttributes;
      body: HTMLAttributes;
      br: HTMLAttributes;
      button: ButtonHTMLAttributes;
      canvas: CanvasHTMLAttributes;
      caption: HTMLAttributes;
      cite: HTMLAttributes;
      code: HTMLAttributes;
      col: ColHTMLAttributes;
      colgroup: ColgroupHTMLAttributes;
      data: DataHTMLAttributes;
      datalist: HTMLAttributes;
      dd: HTMLAttributes;
      del: DelHTMLAttributes;
      details: DetailsHTMLAttributes;
      dfn: HTMLAttributes;
      dialog: DialogHTMLAttributes;
      div: HTMLAttributes;
      dl: HTMLAttributes;
      dt: HTMLAttributes;
      em: HTMLAttributes;
      embed: EmbedHTMLAttributes;
      fieldset: FieldsetHTMLAttributes;
      figcaption: HTMLAttributes;
      figure: HTMLAttributes;
      footer: HTMLAttributes;
      form: FormHTMLAttributes;
      h1: HTMLAttributes;
      h2: HTMLAttributes;
      h3: HTMLAttributes;
      h4: HTMLAttributes;
      h5: HTMLAttributes;
      h6: HTMLAttributes;
      head: HTMLAttributes;
      header: HTMLAttributes;
      hgroup: HTMLAttributes;
      hr: HTMLAttributes;
      html: HtmlHTMLAttributes;
      i: HTMLAttributes;
      iframe: IframeHTMLAttributes;
      img: ImgHTMLAttributes;
      input: InputHTMLAttributes;
      ins: InsHTMLAttributes;
      kbd: HTMLAttributes;
      label: LabelHTMLAttributes;
      legend: HTMLAttributes;
      li: LiHTMLAttributes;
      link: LinkHTMLAttributes;
      main: HTMLAttributes;
      map: MapHTMLAttributes;
      mark: HTMLAttributes;
      menu: MenuHTMLAttributes;
      meta: MetaHTMLAttributes;
      meter: MeterHTMLAttributes;
      nav: HTMLAttributes;
      noscript: HTMLAttributes;
      object: ObjectHTMLAttributes;
      ol: OlHTMLAttributes;
      optgroup: OptgroupHTMLAttributes;
      option: OptionHTMLAttributes;
      output: OutputHTMLAttributes;
      p: HTMLAttributes;
      picture: HTMLAttributes;
      pre: HTMLAttributes;
      progress: ProgressHTMLAttributes;
      q: QuoteHTMLAttributes;
      rp: HTMLAttributes;
      rt: HTMLAttributes;
      ruby: HTMLAttributes;
      s: HTMLAttributes;
      samp: HTMLAttributes;
      script: ScriptHTMLAttributes;
      section: HTMLAttributes;
      select: SelectHTMLAttributes;
      slot: SlotHTMLAttributes;
      small: HTMLAttributes;
      source: SourceHTMLAttributes;
      span: HTMLAttributes;
      strong: HTMLAttributes;
      style: StyleHTMLAttributes;
      sub: HTMLAttributes;
      summary: HTMLAttributes;
      sup: HTMLAttributes;
      svg: SVGAttributes;
      table: TableHTMLAttributes;
      tbody: HTMLAttributes;
      td: TdHTMLAttributes;
      template: HTMLAttributes;
      textarea: TextareaHTMLAttributes;
      tfoot: HTMLAttributes;
      th: ThHTMLAttributes;
      thead: HTMLAttributes;
      time: TimeHTMLAttributes;
      title: HTMLAttributes;
      tr: HTMLAttributes;
      track: TrackHTMLAttributes;
      u: HTMLAttributes;
      ul: HTMLAttributes;
      var: HTMLAttributes;
      video: VideoHTMLAttributes;
      wbr: HTMLAttributes;
      line: SVGAttributes;
      path: SVGAttributes;
      circle: SVGAttributes;
      rect: SVGAttributes;
      g: SVGAttributes;
    }

    interface ImageProps {
      src: string;
      alt: string;
      width?: number;
      height?: number;
      loading?: "lazy" | "eager";
      className?: string;
      quality?: number;
      sizes?: string;
      [key: string]: any;
    }

    interface LinkProps extends AnchorHTMLAttributes {
      href: string;
      className?: string;
      external?: boolean;
      children?: any;
      /** Prefetch the target page on hover/focus/viewport entry (default: true). */
      prefetch?: boolean;
      /** Replace the current history entry instead of pushing a new one. */
      replace?: boolean;
      /** Scroll to the top on navigation (default: true). */
      scroll?: boolean;
      /** Open in a new tab. */
      target?: "_blank" | "_self" | "_parent" | "_top" | string;
      rel?: string;
      title?: string;
      id?: string;
    }

    interface ElementAttributesProperty {
        __jsxProps: {};
    }
  }

  function Icon(props: { name: string } & JSX.SVGAttributes): JSX.Element;
  function Head(props: { children?: any }): JSX.Element;
  function Suspense(props: { children?: any; fallback?: any }): JSX.Element;
  function Link(props: JSX.LinkProps): JSX.Element;
  interface HTMLImageElement {
    __jsxProps: JSX.ImageProps;
  }
}

export {};
