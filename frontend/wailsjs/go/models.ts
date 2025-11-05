export namespace main {
	
	export class HubConfig {
	    Host: string;
	    UseTLS: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HubConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Host = source["Host"];
	        this.UseTLS = source["UseTLS"];
	    }
	}

}

